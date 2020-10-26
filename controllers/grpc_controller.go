/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"net/http"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"time"

	grpcv1alpha1 "github.com/panyuenlau/grpc-operator/api/v1alpha1"
)

// GrpcReconciler reconciles a Grpc object
type GrpcReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	nodePortNum                 int32 = 30010
	grpcClientPort                    = 8081
	grpcClientContainerPortName       = "grpc-client"
)

var (
	performServerCheck = true
)

// +kubebuilder:rbac:groups=grpc.example.com,resources=grpcclients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=grpc.example.com,resources=grpcclients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=grpc.example.com,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=grpc.example.com,resources=pods, verbs=get;lists
func (r *GrpcReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("grpc-client", req.NamespacedName)

	// Fetch the Grpc instance
	grpcClient := &grpcv1alpha1.Grpc{}
	err := r.Get(ctx, req.NamespacedName, grpcClient)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could've been deleted after reconcile request
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers
			log.Info("Grpc resource not found. Ignoring since object must be deleted")

			return ctrl.Result{}, nil
		}
		// Error reading the object -> requeue the request
		log.Error(err, "Failed to get grpc client")
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: grpcClient.Name, Namespace: grpcClient.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// define a new deployment
		dep := r.deploymentForGrpcClient(grpcClient)
		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace,
			"Deployment.Name", dep.Name)
		err = r.Create(ctx, dep)

		if err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace",
				dep.Namespace, "Deployment.Name", dep.Name)
			// Reconcile failed due to error - requeue
			return ctrl.Result{}, err
		}

		// Deployment created successfully -> return and requeue
		return ctrl.Result{Requeue: true}, err
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Reconcile failed due to error - requeue
		return ctrl.Result{}, err
	}

	// Check if the service already exists, if not create a new one
	foundService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: grpcClient.Name, Namespace: grpcClient.Namespace}, foundService)
	if err != nil && errors.IsNotFound(err) {
		svc := r.serviceForGrpcClient(grpcClient)
		log.Info("Creating a new Service", "Service.Namespace", svc.Namespace,
			"Service.Name", svc.Name)
		err = r.Create(ctx, svc)

		if err != nil {
			log.Error(err, "Failed to create new Service", "Service.Namespace",
				svc.Namespace, "Service.Name", svc.Name)
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, err
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	// Ensure the deployment size is the same as the spec
	size := grpcClient.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.Update(ctx, found) //Update updates the given obj in the Kubernetes cluster
		if err != nil {
			log.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace,
				"Deployment.Name", found.Name)
			return ctrl.Result{}, err
		}
		// Requeue for any reason other than an error
		// Spec updated -> return and requeue
		return ctrl.Result{Requeue: true}, nil
	}

	// Update the Grpc status with the pod names
	// List the pods for this grpcClient's deployment
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(grpcClient.Namespace),
		client.MatchingLabels(labelsForGrpcClient(grpcClient.Name)),
	}

	if err = r.List(ctx, podList, listOpts...); err != nil {
		log.Error(err, "Failed to list pods", "GrpcClient.Namespace", grpcClient.Namespace,
			"GrpcClient.name", grpcClient.Name)
		// Requeue for any reason other than an error
		return ctrl.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, grpcClient.Status.Nodes) {
		grpcClient.Status.Nodes = podNames
		err := r.Status().Update(ctx, grpcClient)
		if err != nil {
			log.Error(err, "Failed to update GrpcClient status")
			// Requeue for any reason other than an error
			return ctrl.Result{}, err
		}
	}

	// When the program enters the reconcile in the first time, creat a goroutine to keep track of server status
	if performServerCheck {
		performServerCheck = false
		go r.checkServerStatus(grpcClient)
	}

	// Reconcile successful -> don't requeue
	return ctrl.Result{}, nil
}

func (r GrpcReconciler) checkServerStatus(grpcClient *grpcv1alpha1.Grpc) {
	log := ctrl.Log.WithName("controllers").WithName("serverStatus")
	ctx := context.Background()

	checkStatusFreq := 5
	retryStatusFreq := 10

	for {
		//resp, errHTTP := http.Get("http://localhost:" + strconv.Itoa(int(nodePort)) + "/serverstatus")

		grpcClientAddr := "http://localhost:" + strconv.Itoa(int(nodePortNum)) + "/serverstatus"

		resp, errHTTP := http.Get(grpcClientAddr)

		// If the server is currently down
		if errHTTP != nil || resp.StatusCode != 200 {
			log.Info("The gRPC server is down!")
			// Only perform the update if current status is different from previous
			if grpcClient.Status.ServerStatus == "running" {
				grpcClient.Status.ServerStatus = "not running"
				if err := r.Status().Update(ctx, grpcClient); err != nil {
					log.Error(err, "Failed to update GrpcClient status")
				}
			}

			time.Sleep(time.Duration(retryStatusFreq) * time.Second)
			continue
		}

		// If the server is up and running properly
		if grpcClient.Status.ServerStatus != "running" {
			grpcClient.Status.ServerStatus = "running"
			if err := r.Status().Update(ctx, grpcClient); err != nil {
				log.Error(err, "Failed to update GrpcClient status")
			}
		}

		defer resp.Body.Close()

		time.Sleep(time.Duration(checkStatusFreq) * time.Second)
	}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

// Make the deployment for the caller
func (r *GrpcReconciler) deploymentForGrpcClient(grpcClient *grpcv1alpha1.Grpc) *appsv1.Deployment {
	ls := labelsForGrpcClient(grpcClient.Name)
	replicas := grpcClient.Spec.Size
	log := r.Log.WithValues("grpc-client-deployment", grpcClient.Namespace)

	// variables used to define the Pod specification
	containerImage := grpcClient.Spec.Image
	containerProtocol := grpcClient.Spec.Protocol
	containerName := "grpc-client"
	var containerPort int32 = 8081 // the port that the container is listening to?

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      grpcClient.Name,
			Namespace: grpcClient.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			// A label selector is a label query over a set of resources
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				// ObjectMeta is metadata that all persisted resources must have,
				// which includes all objects users must create.
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: containerImage,
						Name:  containerName,
						// ContainerPort represents a network port in a single container
						Ports: []corev1.ContainerPort{{
							ContainerPort: containerPort,
							// Each named port in a pod must have a unique name. Name for the port that can be
							// referred to by services.
							Name:     grpcClientContainerPortName,
							Protocol: containerProtocol,
						}},
					}},
				},
			},
		},
	}

	// Set grpcClient instance as the owner and controller, using the reconciler scheme
	err := ctrl.SetControllerReference(grpcClient, deployment, r.Scheme)
	if err != nil {
		log.Error(err, "Failed to set controller reference for the grpc client deployment:", err)
		return nil
	}

	return deployment
}

// Create the service for operator to reach to grpc-client pods
func (r *GrpcReconciler) serviceForGrpcClient(grpcClient *grpcv1alpha1.Grpc) *corev1.Service {
	ls := labelsForGrpcClient(grpcClient.Name)
	log := r.Log.WithValues("operator-client-service", grpcClient.Namespace)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      grpcClient.Name,
			Namespace: grpcClient.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					NodePort: nodePortNum,
					Port:     8081,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						StrVal: grpcClientContainerPortName,
					},
				},
			},
		},
	}

	err := ctrl.SetControllerReference(grpcClient, svc, r.Scheme)
	if err != nil {
		log.Error(err, "Failed to set controller reference for the grpc client service:", err)
		return nil
	}

	return svc
}

// labelsForGrpcClient returns the label names as a map in the format of "app": "grpc-client", "grpc_cr" []
func labelsForGrpcClient(name string) map[string]string {
	return map[string]string{"app": "grpc-client", "grpc_cr": name}
}

func (r *GrpcReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&grpcv1alpha1.Grpc{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
