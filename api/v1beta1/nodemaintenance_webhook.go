/*
Copyright 2021.

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

package v1beta1

import (
	"context"
	"fmt"

	"github.com/medik8s/common/pkg/etcd"
	"github.com/medik8s/common/pkg/nodes"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ErrorNodeNotExists               = "invalid nodeName, no node with name %s found"
	ErrorNodeMaintenanceExists       = "invalid nodeName, a NodeMaintenance for node %s already exists"
	ErrorControlPlaneQuorumViolation = "can not put master/control-plane node into maintenance at this moment, disrupting node %s will violate etcd quorum"
	ErrorNodeNameUpdateForbidden     = "updating spec.NodeName isn't allowed"
)

const (
	EtcdQuorumPDBNewName   = "etcd-guard-pdb"    // The new name of the PDB - From OCP 4.11
	EtcdQuorumPDBOldName   = "etcd-quorum-guard" // The old name of the PDB - Up to OCP 4.10
	EtcdQuorumPDBNamespace = "openshift-etcd"
)

// log is for logging in this package.
var nodemaintenancelog = logf.Log.WithName("nodemaintenance-resource")

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// NodeMaintenanceValidator validates NodeMaintenance resources. Needed because we need a client for validation
// +k8s:deepcopy-gen=false
type NodeMaintenanceValidator struct {
	client client.Client
}

var validator *NodeMaintenanceValidator

func (r *NodeMaintenance) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// init the validator!
	validator = &NodeMaintenanceValidator{
		client: mgr.GetClient(),
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-nodemaintenance-medik8s-io-v1beta1-nodemaintenance,mutating=false,failurePolicy=fail,sideEffects=None,groups=nodemaintenance.medik8s.io,resources=nodemaintenances,verbs=create;update,versions=v1beta1,name=vnodemaintenance.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &NodeMaintenance{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *NodeMaintenance) ValidateCreate() (admission.Warnings, error) {
	nodemaintenancelog.Info("validate create", "name", r.Name)

	if validator == nil {
		return nil, fmt.Errorf("nodemaintenance validator isn't initialized yet")
	}
	return nil, validator.ValidateCreate(r)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *NodeMaintenance) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	nodemaintenancelog.Info("validate update", "name", r.Name)

	if validator == nil {
		return nil, fmt.Errorf("nodemaintenance validator isn't initialized yet")
	}
	return nil, validator.ValidateUpdate(r, old.(*NodeMaintenance))
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *NodeMaintenance) ValidateDelete() (admission.Warnings, error) {
	nodemaintenancelog.Info("validate delete", "name", r.Name)

	if validator == nil {
		return nil, fmt.Errorf("nodemaintenance validator isn't initialized yet")
	}
	return nil, nil
}

func (v *NodeMaintenanceValidator) ValidateCreate(nm *NodeMaintenance) error {
	// Validate that node with given name exists
	nodeName := nm.Spec.NodeName
	if err := v.validateNodeExists(nodeName); err != nil {
		nodemaintenancelog.Info("validation failed ", "nmName", nm.Name, "nodeName", nodeName, "error", err)
		return err
	}

	// Validate that no NodeMaintenance for given node exists yet
	if err := v.validateNoNodeMaintenanceExists(nodeName); err != nil {
		nodemaintenancelog.Info("validation failed", "nmName", nm.Name, "nodeName", nodeName, "error", err)
		return err
	}

	// Validate that NodeMaintenance for control-plane nodes don't violate quorum
	if err := v.validateControlPlaneQuorum(nodeName); err != nil {
		nodemaintenancelog.Info("validation failed", "nmName", nm.Name, "nodeName", nodeName, "error", err)
		return err
	}

	return nil
}

func (v *NodeMaintenanceValidator) ValidateUpdate(new, old *NodeMaintenance) error {
	// Validate that node name didn't change
	if new.Spec.NodeName != old.Spec.NodeName {
		nodemaintenancelog.Info("validation failed", "error", ErrorNodeNameUpdateForbidden)
		return fmt.Errorf(ErrorNodeNameUpdateForbidden)
	}
	return nil
}

func (v *NodeMaintenanceValidator) validateNodeExists(nodeName string) error {
	if node, err := getNode(nodeName, v.client); err != nil {
		return fmt.Errorf("could not get node for validating spec.NodeName, please try again: %v", err)
	} else if node == nil {
		return fmt.Errorf(ErrorNodeNotExists, nodeName)
	}
	return nil
}

func (v *NodeMaintenanceValidator) validateNoNodeMaintenanceExists(nodeName string) error {
	var nodeMaintenances NodeMaintenanceList
	if err := v.client.List(context.TODO(), &nodeMaintenances, &client.ListOptions{}); err != nil {
		return fmt.Errorf("could not list NodeMaintenances for validating spec.NodeName, please try again: %v", err)
	}

	for _, nm := range nodeMaintenances.Items {
		if nm.Spec.NodeName == nodeName {
			return fmt.Errorf(ErrorNodeMaintenanceExists, nodeName)
		}
	}

	return nil
}

func (v *NodeMaintenanceValidator) validateControlPlaneQuorum(nodeName string) error {
	// check if the node is a control-plane node
	node, err := getNode(nodeName, v.client)
	if err != nil {
		return fmt.Errorf("could not get node for master/control-plane quorum validation, please try again: %v", err)
	} else if node == nil {
		// this should have been catched already, but just in case
		return fmt.Errorf(ErrorNodeNotExists, nodeName)
	} else if !nodes.IsControlPlane(node) {
		// not a control-plane node, nothing to do
		return nil
	}
	canDisrupt, err := etcd.IsEtcdDisruptionAllowed(context.Background(), v.client, nodemaintenancelog, node)
	if err != nil {
		return err
	}
	if !canDisrupt {
		return fmt.Errorf(ErrorControlPlaneQuorumViolation, nodeName)
	}
	return nil
}

// if the returned node is nil, it wasn't found
func getNode(nodeName string, client client.Client) (*v1.Node, error) {
	var node v1.Node
	key := types.NamespacedName{
		Name: nodeName,
	}
	if err := client.Get(context.TODO(), key, &node); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not get node: %v", err)
	}
	return &node, nil
}
