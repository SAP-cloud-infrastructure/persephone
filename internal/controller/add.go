package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-operator/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/config"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller/internalsecret"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller/project"
	"github.com/sap-cloud-infrastructure/persephone/internal/controller/shoot"
	openstacklocal "github.com/sap-cloud-infrastructure/persephone/internal/openstack"
)

// AddToManager adds all controllers to the given manager.
func AddToManager(ctx context.Context, mgr manager.Manager, opts *options.Options, cfg config.PersephoneConfig) error {
	openStackClientSet, err := openstacklocal.NewRegionOpenStackClientSet(ctx, cfg.Regions, cfg.ClusterUsersDomain)
	if err != nil {
		return fmt.Errorf("failed creating OpenStack clientset: %w", err)
	}

	if err := (&project.Reconciler{
		PersephoneConfig: cfg,
	}).AddToManager(mgr, opts.Controllers.Project.MaxConcurrentReconciles); err != nil {
		return fmt.Errorf("failed adding %s controller: %w", project.ControllerName, err)
	}

	if err := (&internalsecret.Reconciler{
		OpenStackClientSet: openStackClientSet,
		LandscapeName:      cfg.LandscapeName,
	}).AddToManager(mgr, opts.Controllers.InternalSecret.MaxConcurrentReconciles); err != nil {
		return fmt.Errorf("failed adding %s controller: %w", internalsecret.ControllerName, err)
	}

	if err := (&shoot.Reconciler{
		OpenStackClientSet: openStackClientSet,
		LandscapeName:      cfg.LandscapeName,
	}).AddToManager(mgr, opts.Controllers.Shoot.MaxConcurrentReconciles); err != nil {
		return fmt.Errorf("failed adding %s controller: %w", shoot.ControllerName, err)
	}

	return nil
}
