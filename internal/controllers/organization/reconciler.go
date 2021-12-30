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

package organization

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/yndd/ndd-runtime/pkg/event"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/nddo-runtime/pkg/reconciler/managed"
	"github.com/yndd/nddo-runtime/pkg/resource"
	orgv1alpha1 "github.com/yndd/nddr-organization/apis/org/v1alpha1"
	"github.com/yndd/nddr-organization/internal/shared"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	// timers
	reconcileTimeout = 1 * time.Minute
	veryShortWait    = 1 * time.Second
	// errors
	errUnexpectedResource = "unexpected organization object"
	errGetK8sResource     = "cannot get organization resource"
)

// Setup adds a controller that reconciles infra.
func Setup(mgr ctrl.Manager, o controller.Options, nddcopts *shared.NddControllerOptions) error {
	name := "nddo/" + strings.ToLower(orgv1alpha1.OrganizationGroupKind)
	orgfn := func() orgv1alpha1.Org { return &orgv1alpha1.Organization{} }

	speedy := make(map[string]int)

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(orgv1alpha1.OrganizationGroupVersionKind),
		managed.WithLogger(nddcopts.Logger.WithValues("controller", name)),
		managed.WithApplication(&application{
			client: resource.ClientApplicator{
				Client:     mgr.GetClient(),
				Applicator: resource.NewAPIPatchingApplicator(mgr.GetClient()),
			},
			log:    nddcopts.Logger.WithValues("applogic", name),
			newOrg: orgfn,
			speedy: speedy,
		}),
		managed.WithSpeedy(speedy),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(&orgv1alpha1.Organization{}).
		Owns(&orgv1alpha1.Organization{}).
		WithEventFilter(resource.IgnoreUpdateWithoutGenerationChangePredicate()).
		WithEventFilter(resource.IgnoreUpdateWithoutGenerationChangePredicate()).
		Complete(r)

}

type application struct {
	client resource.ClientApplicator
	log    logging.Logger

	newOrg func() orgv1alpha1.Org

	speedy map[string]int

	speedyMutex sync.Mutex
}

func getCrName(cr orgv1alpha1.Org) string {
	return strings.Join([]string{cr.GetNamespace(), cr.GetName()}, ".")
}

func (r *application) Initialize(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*orgv1alpha1.Organization)
	if !ok {
		return errors.New(errUnexpectedResource)
	}

	if err := cr.InitializeResource(); err != nil {
		r.log.Debug("Cannot initialize", "error", err)
		return err
	}

	return nil
}

func (r *application) Update(ctx context.Context, mg resource.Managed) (map[string]string, error) {
	cr, ok := mg.(*orgv1alpha1.Organization)
	if !ok {
		return nil, errors.New(errUnexpectedResource)
	}

	return r.handleAppLogic(ctx, cr)
}

func (r *application) FinalUpdate(ctx context.Context, mg resource.Managed) {
}

func (r *application) Timeout(ctx context.Context, mg resource.Managed) time.Duration {
	cr, _ := mg.(*orgv1alpha1.Organization)
	crName := getCrName(cr)
	r.speedyMutex.Lock()
	speedy := r.speedy[crName]
	r.speedyMutex.Unlock()
	if speedy <= 5 {
		r.log.Debug("Speedy", "number", speedy)
		speedy++
		return veryShortWait
	}
	return reconcileTimeout
}

func (r *application) Delete(ctx context.Context, mg resource.Managed) (bool, error) {
	return true, nil
}

func (r *application) FinalDelete(ctx context.Context, mg resource.Managed) {
	cr, _ := mg.(*orgv1alpha1.Organization)
	crName := getCrName(cr)
	r.speedyMutex.Lock()
	delete(r.speedy, crName)
	r.speedyMutex.Unlock()
}

func (r *application) handleAppLogic(ctx context.Context, cr orgv1alpha1.Org) (map[string]string, error) {
	log := r.log.WithValues("function", "handleAppLogic", "crname", cr.GetName())
	log.Debug("handleAppLogic")

	depRegister := cr.GetRegister()
	cr.SetStatus("up")
	cr.SetReason("")
	cr.SetStateRegister(depRegister)
	return make(map[string]string), nil
}
