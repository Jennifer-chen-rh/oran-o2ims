/*
Copyright 2023.

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
	"encoding/json"
	"fmt"

	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/files"
)

// ClusterTemplateReconciler reconciles a ClusterTemplate object
type ClusterTemplateReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterTemplateReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *oranv1alpha1.ClusterTemplate
}

//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oran.openshift.io,resources=clustertemplates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterTemplate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile

func (r *ClusterTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (
	result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// Fetch the object:
	object := &oranv1alpha1.ClusterTemplate{}
	if err := r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ClusterTemplate",
			slog.String("error", err.Error()),
		)
	}

	r.Logger.InfoContext(ctx, "[Reconcile Cluster Template] "+object.Name)

	// Create and run the task:
	task := &clusterTemplateReconcilerTask{
		logger: r.Logger,
		client: r.Client,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterTemplateReconcilerTask) run(ctx context.Context) (nextReconcile ctrl.Result, err error) {
	// Check if the inputDataSchema is in a JSON format; the schema itself is not of importance.
	validationErr := t.validateInputDataSchema()

	// If there is an error, log it and return with error.
	if validationErr != nil {
		t.logger.ErrorContext(
			ctx,
			"inputDataSchema is not in a JSON format",
		)
		validationErr = fmt.Errorf(
			"failed to validate inputDataSchema: %s", validationErr.Error())
	}

	// Update the ClusterTemplate status.
	err = t.updateClusterTemplateStatus(ctx, validationErr)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update status for ClusterTemplate",
			slog.String("name", t.object.Name),
		)
		return
	}

	return
}

// validateInputDataSchema succeeds if intputDataSchema is in a JSON format.
func (t *clusterTemplateReconcilerTask) validateInputDataSchema() (err error) {

	var jsonInputDataSchema json.RawMessage
	return json.Unmarshal([]byte(t.object.Spec.InputDataSchema), &jsonInputDataSchema)
}

// updateClusterTemplateStatus update the status of the ClusterTemplate object (CR).
func (t *clusterTemplateReconcilerTask) updateClusterTemplateStatus(
	ctx context.Context, inputError error) error {

	t.object.Status.ClusterTemplateValidation.ClusterTemplateIsValid = true
	t.object.Status.ClusterTemplateValidation.ClusterTemplateError = ""

	if inputError != nil {
		t.object.Status.ClusterTemplateValidation.ClusterTemplateIsValid = false
		t.object.Status.ClusterTemplateValidation.ClusterTemplateError = inputError.Error()
	}

	return utils.UpdateK8sCRStatus(ctx, t.client, t.object)
}

// initConfigmapClusterInstanceTemplate creates the configmap for cluster instance template
func (r *ClusterTemplateReconciler) initConfigmapClusterInstanceTemplate() (err error) {
	oranNs := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.ORANO2IMSNamespace,
		},
	}
	err = r.Create(context.TODO(), &oranNs)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return
		}
	}

	// Load the template from yaml file
	clusterInstanceTmpl, err := files.Controllers.ReadFile(utils.ClusterInstanceTemplatePath)
	if err != nil {
		err = fmt.Errorf("error reading template file: %w", err)
		return
	}

	// Create immutable configmap
	immutable := true
	clusterInstanceTmplCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.ClusterInstanceTemplateConfigmapName,
			Namespace: utils.ClusterInstanceTemplateConfigmapNamespace,
		},
		Immutable: &immutable,
		Data: map[string]string{
			utils.ClusterInstanceTemplateConfigmapName: string(clusterInstanceTmpl),
		},
	}

	if err = r.Delete(context.TODO(), clusterInstanceTmplCm); err != nil {
		if !errors.IsNotFound(err) {
			return
		}
	}

	if err = r.Create(context.TODO(), clusterInstanceTmplCm); err != nil {
		return
	}

	r.Logger.Info(
		"Created the ClusterInstance template configmap",
		slog.String("name", clusterInstanceTmplCm.Name),
		slog.String("namespace", clusterInstanceTmplCm.Namespace),
	)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.initConfigmapClusterInstanceTemplate(); err != nil {
		r.Logger.Error(
			"Error creating the ClusterInstance template configmap",
			slog.String("error", err.Error()),
		)
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("orano2ims-cluster-template").
		For(&oranv1alpha1.ClusterTemplate{},
			// Watch for create and update event for ClusterTemplate.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
