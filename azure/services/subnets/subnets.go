/*
Copyright 2019 The Kubernetes Authors.

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

package subnets

import (
	"context"
	"fmt"

	asonetworkv1 "github.com/Azure/azure-service-operator/v2/api/network/v1api20201101"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/aso"
	"sigs.k8s.io/cluster-api-provider-azure/util/slice"
)

const serviceName = "subnets"

// SubnetScope defines the scope interface for a subnet service.
type SubnetScope interface {
	aso.Scope
	UpdateSubnetID(string, string)
	UpdateSubnetCIDRs(string, []string)
	SubnetSpecs() []azure.ASOResourceSpecGetter[*asonetworkv1.VirtualNetworksSubnet]
}

// New creates a new service.
func New(scope SubnetScope) *aso.Service[*asonetworkv1.VirtualNetworksSubnet, SubnetScope] {
	svc := aso.NewService[*asonetworkv1.VirtualNetworksSubnet, SubnetScope](serviceName, scope)
	svc.ListFunc = list
	svc.Specs = scope.SubnetSpecs()
	svc.ConditionType = infrav1.SubnetsReadyCondition
	svc.PostCreateOrUpdateResourceHook = postCreateOrUpdateResourceHook
	return svc
}

func postCreateOrUpdateResourceHook(_ context.Context, scope SubnetScope, subnet *asonetworkv1.VirtualNetworksSubnet, err error) error {
	if err != nil {
		return err
	}

	if subnet.Status.ProvisioningState != nil && *subnet.Status.ProvisioningState == asonetworkv1.ProvisioningState_STATUS_Succeeded {
		name := subnet.AzureName()
		fmt.Println("AZURE NAME IN POSTCREATEORUPDATERESOURCEHOOK", name)
		fmt.Println("SUBNET NAME IN POSTCREATEORUPDATERESOURCEHOOK", subnet.Name)
		// update the status of the capz azurecluster resource
		// HACKTEST: only call UpdateSubnetID and UpdateSubnetCIDRs if azurecluster's CIDR == subnet.Status.AddressPrefix
		// if default is getting called less and no longer are the cidr blocks empty then this is the bug
		// scope.UpdateSubnetID(name, ptr.Deref(subnet.Status.Id, ""))
		// scope.UpdateSubnetCIDRs(name, converters.GetSubnetAddresses(*subnet)) //updated this to be empty

		if subnet.Status.AddressPrefix == nil {
			fmt.Println("AddressPrefix is nil for subnet:", subnet.AzureName())
			return nil
		}
		actualCIDR := *subnet.Status.AddressPrefix

		// get the desired CIDRs for the subnet from the AzureCluster spec
		var desiredCIDR string
		found := false
		for _, spec := range scope.SubnetSpecs() {
			subnetSpec, ok := spec.(*SubnetSpec)
			if !ok {
				continue
			}
			if subnetSpec.Name == name {
				if len(subnetSpec.CIDRs) > 0 {
					desiredCIDR = subnetSpec.CIDRs[0] // Use the first CIDR block
					found = true
				}
				break
			}
		}

		// in the case no matching subnet is found
		if !found {
			fmt.Println("No matching subnet found in AzureCluster spec for subnet:", name)
			return nil
		}

		fmt.Println("Desired Spec CIDR:", desiredCIDR)
		fmt.Println("Actual Status CIDR:", actualCIDR)

		if desiredCIDR == actualCIDR {
			fmt.Println("CIDRs match for subnet:", subnet.AzureName())
			scope.UpdateSubnetID(name, ptr.Deref(subnet.Status.Id, ""))
			scope.UpdateSubnetCIDRs(name, []string{actualCIDR})
		} else {
			fmt.Printf("CIDRs do not match for subnet: %s. Desired: %s, Actual: %s\n", subnet.AzureName(), desiredCIDR, actualCIDR)
		}
	}
	return nil
}

func list(ctx context.Context, client client.Client, opts ...client.ListOption) ([]*asonetworkv1.VirtualNetworksSubnet, error) {
	list := &asonetworkv1.VirtualNetworksSubnetList{}
	err := client.List(ctx, list, opts...)
	return slice.ToPtrs(list.Items), err
}
