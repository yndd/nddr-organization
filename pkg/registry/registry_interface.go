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

	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/nddo-grpc/resource/resourcepb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Option can be used to manipulate Options.
type Option func(Registry)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) Option {
	return func(s Registry) {
		s.WithLogger(log)
	}
}

func WithClient(c client.Client) Option {
	return func(s Registry) {
		s.WithClient(c)
	}
}

type Registry interface {
	WithLogger(logging.Logger)
	WithClient(client.Client)
	GetRegisterName(string, string) string
	GetRegister(context.Context, string, string) (map[string]string, error)
	GetRegistryClient(ctx context.Context, registerName string) (resourcepb.ResourceClient, error)
}
