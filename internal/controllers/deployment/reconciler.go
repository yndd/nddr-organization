/*
Copyright 2021 NDD.

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

package deployment

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
	"github.com/yndd/ndd-runtime/pkg/event"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-runtime/pkg/resource"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	orgv1alpha1 "github.com/yndd/nddr-organization/apis/org/v1alpha1"
	"github.com/yndd/nddr-organization/internal/shared"
)

const (
	finalizerName = "finalizer.deployment.org.nddr.yndd.io"
	//
	reconcileTimeout = 1 * time.Minute
	longWait         = 1 * time.Minute
	mediumWait       = 30 * time.Second
	shortWait        = 15 * time.Second
	veryShortWait    = 5 * time.Second

	// Errors
	errGetK8sResource = "cannot get organization resource"
	errUpdateStatus   = "cannot update status of organization resource"

	// events
	reasonReconcileSuccess      event.Reason = "ReconcileSuccess"
	reasonCannotDelete          event.Reason = "CannotDeleteResource"
	reasonCannotAddFInalizer    event.Reason = "CannotAddFinalizer"
	reasonCannotDeleteFInalizer event.Reason = "CannotDeleteFinalizer"
	reasonCannotInitialize      event.Reason = "CannotInitializeResource"
	reasonCannotGetAllocations  event.Reason = "CannotGetAllocations"
	reasonAppLogicFailed        event.Reason = "ApplogicFailed"
)

// ReconcilerOption is used to configure the Reconciler.
type ReconcilerOption func(*Reconciler)

// Reconciler reconciles packages.
type Reconciler struct {
	client  resource.ClientApplicator
	log     logging.Logger
	record  event.Recorder
	managed mrManaged

	newDeployment       func() orgv1alpha1.Dp
	newOrganizationList func() orgv1alpha1.OrgList
}

type mrManaged struct {
	resource.Finalizer
}

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		r.log = log
	}
}

func WithNewReourceFn(f func() orgv1alpha1.Dp) ReconcilerOption {
	return func(r *Reconciler) {
		r.newDeployment = f
	}
}

func WithNewOrganizationListFn(f func() orgv1alpha1.OrgList) ReconcilerOption {
	return func(r *Reconciler) {
		r.newOrganizationList = f
	}
}

// WithRecorder specifies how the Reconciler should record Kubernetes events.
func WithRecorder(er event.Recorder) ReconcilerOption {
	return func(r *Reconciler) {
		r.record = er
	}
}

func defaultMRManaged(m ctrl.Manager) mrManaged {
	return mrManaged{
		Finalizer: resource.NewAPIFinalizer(m.GetClient(), finalizerName),
	}
}

// Setup adds a controller that reconciles Topology.
func Setup(mgr ctrl.Manager, o controller.Options, nddcopts *shared.NddControllerOptions) error {
	name := "nddr/" + strings.ToLower(orgv1alpha1.DeploymentGroupKind)
	fn := func() orgv1alpha1.Dp { return &orgv1alpha1.Deployment{} }
	orglfn := func() orgv1alpha1.OrgList { return &orgv1alpha1.OrganizationList{} }

	r := NewReconciler(mgr,
		WithLogger(nddcopts.Logger.WithValues("controller", name)),
		WithNewReourceFn(fn),
		WithNewOrganizationListFn(orglfn),
		WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
	)

	orgHandler := &EnqueueRequestForAllOrganizations{
		client: mgr.GetClient(),
		log:    nddcopts.Logger,
		ctx:    context.Background(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(&orgv1alpha1.Deployment{}).
		WithEventFilter(resource.IgnoreUpdateWithoutGenerationChangePredicate()).
		Watches(&source.Kind{Type: &orgv1alpha1.Organization{}}, orgHandler).
		Complete(r)
}

// NewReconciler creates a new reconciler.
func NewReconciler(mgr ctrl.Manager, opts ...ReconcilerOption) *Reconciler {

	r := &Reconciler{
		client: resource.ClientApplicator{
			Client:     mgr.GetClient(),
			Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient()),
		},
		log:     logging.NewNopLogger(),
		record:  event.NewNopRecorder(),
		managed: defaultMRManaged(mgr),
	}

	for _, f := range opts {
		f(r)
	}

	return r
}

// Reconcile ipam allocation.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) { // nolint:gocyclo
	log := r.log.WithValues("request", req)
	log.Debug("Reconciling topology", "NameSpace", req.NamespacedName)

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	cr := r.newDeployment()
	if err := r.client.Get(ctx, req.NamespacedName, cr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		log.Debug("Cannot get managed resource", "error", err)
		return reconcile.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetK8sResource)
	}
	record := r.record.WithAnnotations("name", cr.GetAnnotations()[cr.GetName()])

	if meta.WasDeleted(cr) {
		log = log.WithValues("deletion-timestamp", cr.GetDeletionTimestamp())

		if err := r.managed.RemoveFinalizer(ctx, cr); err != nil {
			// If this is the first time we encounter this issue we'll be
			// requeued implicitly when we update our status with the new error
			// condition. If not, we requeue explicitly, which will trigger
			// backoff.
			record.Event(cr, event.Warning(reasonCannotDeleteFInalizer, err))
			log.Debug("Cannot remove managed resource finalizer", "error", err)
			cr.SetConditions(nddv1.ReconcileError(err), orgv1alpha1.NotReady())
			return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, cr), errUpdateStatus)
		}

		// We've successfully delete our resource (if necessary) and
		// removed our finalizer. If we assume we were the only controller that
		// added a finalizer to this resource then it should no longer exist and
		// thus there is no point trying to update its status.
		log.Debug("Successfully deleted resource")
		return reconcile.Result{Requeue: false}, nil
	}

	if err := r.managed.AddFinalizer(ctx, cr); err != nil {
		// If this is the first time we encounter this issue we'll be requeued
		// implicitly when we update our status with the new error condition. If
		// not, we requeue explicitly, which will trigger backoff.
		record.Event(cr, event.Warning(reasonCannotAddFInalizer, err))
		log.Debug("Cannot add finalizer", "error", err)
		cr.SetConditions(nddv1.ReconcileError(err), orgv1alpha1.NotReady())
		return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := cr.InitializeResource(); err != nil {
		record.Event(cr, event.Warning(reasonCannotInitialize, err))
		log.Debug("Cannot initialize", "error", err)
		cr.SetConditions(nddv1.ReconcileError(err), orgv1alpha1.NotReady())
		return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.handleAppLogic(ctx, cr); err != nil {
		record.Event(cr, event.Warning(reasonAppLogicFailed, err))
		log.Debug("handle applogic failed", "error", err)
		cr.SetConditions(nddv1.ReconcileError(err), orgv1alpha1.NotReady())
		return reconcile.Result{Requeue: true}, errors.Wrap(r.client.Status().Update(ctx, cr), errUpdateStatus)
	}

	cr.SetConditions(nddv1.ReconcileSuccess(), orgv1alpha1.Ready())
	// we don't need to requeue for topology
	return reconcile.Result{}, errors.Wrap(r.client.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *Reconciler) handleAppLogic(ctx context.Context, cr orgv1alpha1.Dp) error {
	orgs := r.newOrganizationList()
	if err := r.client.List(ctx, orgs); err != nil {
		return err
	}

	orgfound := false
	orgregister := make(map[string]string)
	for _, org := range orgs.GetOrganizations() {
		if org.GetOrganizationName() == cr.GetOrganizationName() {
			orgfound = true
			orgregister = org.GetRegister()
			break
		}
	}
	if !orgfound {
		cr.SetStatus("down")
		cr.SetReason("organization not found")
		cr.SetStateRegister(make(map[string]string))
		return errors.New("organization not found")
	}

	if cr.GetAdminState() == "disable" {
		cr.SetStatus("down")
		cr.SetReason("admin state disabled")
		cr.SetStateRegister(make(map[string]string))
	} else {
		depRegister := getDeploymentRegister(orgregister, cr.GetRegister())
		cr.SetStateRegister(depRegister)
	}

	return nil
}

func getDeploymentRegister(orgRegister, depRegister map[string]string) map[string]string {
	for orgKind, orgName := range orgRegister {
		if _, ok := depRegister[orgKind]; !ok {
			depRegister[orgKind] = orgName
		}
	}
	return depRegister
}
