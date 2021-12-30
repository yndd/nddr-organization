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

package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/yndd/ndd-runtime/pkg/logging"
	orgv1alpha1 "github.com/yndd/nddr-organization/apis/org/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RegisterKind string

const (
	RegisterKindIpam            RegisterKind = "ipam"
	RegisterKindAs              RegisterKind = "as"
	RegisterKindNetworkInstance RegisterKind = "network-instance"
	RegisterKindVlan            RegisterKind = "vlan"
	RegisterKindEndpointGroup   RegisterKind = "endpoint-group"
)

func (r RegisterKind) String() string {
	switch r {
	case RegisterKindIpam:
		return "ipam"
	case RegisterKindAs:
		return "as"
	case RegisterKindNetworkInstance:
		return "network-instance"
	case RegisterKindVlan:
		return "vlan"
	case RegisterKindEndpointGroup:
		return "endpoint-group"
	}
	return "unknown"
}

type registry struct {
	log logging.Logger
	// kubernetes
	client client.Client
}

func New(opts ...Option) Registry {
	s := &registry{}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *registry) WithLogger(log logging.Logger) {
	s.log = log
}

func (s *registry) WithClient(c client.Client) {
	s.client = c
}

func (r *registry) GetRegisterName(organizationName string, deploymentName string) string {
	if deploymentName == "" {
		return organizationName
	}
	return strings.Join([]string{organizationName, deploymentName}, ".")
}

func (r *registry) GetRegister(ctx context.Context, namespace, registerName string) (map[string]string, error) {
	// critical registers are ipam and as right now since they server dynamic
	// grpc services
	criticalRegisters := []string{
		RegisterKindIpam.String(),
		RegisterKindAs.String(),
	}

	var registers map[string]string
	switch len(strings.Split(registerName, ".")) {
	case 2:
		dep := &orgv1alpha1.Deployment{}
		if err := r.client.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      registerName,
		}, dep); err != nil {
			return nil, err
		}
		registers = dep.GetStateRegister()

	case 1:
		org := &orgv1alpha1.Organization{}
		if err := r.client.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      registerName,
		}, org); err != nil {
			return nil, err
		}

		registers = org.GetStateRegister()
	default:
		return nil, fmt.Errorf("wrong input in get register %s", registerName)
	}
	for _, register := range criticalRegisters {
		if _, ok := registers[register]; !ok {
			return nil, fmt.Errorf("critical register %s not found in registry", register)
		}
	}
	return registers, nil
}
