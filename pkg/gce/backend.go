package gce

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"

	corelistersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/ingress-gce/pkg/backends"

	extensions "k8s.io/api/extensions/v1beta1"

	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"

	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"
)

func UpdateBackendService(ctx *gce.GCECloud, serviceLister corelistersv1.ServiceLister, ingressBackend extensions.IngressBackend, id, namespace string, settings map[string]interface{}) error {
	serviceID := getServiceID(ingressBackend, namespace)
	servicePort, err := getServiceProtocol(serviceLister, serviceID)
	if err != nil {
		return err
	}

	version := servicePort.Version()
	backendService, err := getBackendService(id, version, ctx)
	if err != nil {
		return err
	}

	be, err := mergeBackendSettingsWithService(backendService, settings)
	if err != nil {
		return err
	}

	return updateBackendService(version, be, ctx)
}

func getBackendService(name string, version meta.Version, cloud *gce.GCECloud) (*backends.BackendService, error) {
	var gceObj interface{}
	var err error
	if version == meta.VersionAlpha {
		gceObj, err = cloud.GetAlphaGlobalBackendService(name)
		if err != nil {
			return nil, err
		}
	} else {
		gceObj, err = cloud.GetGlobalBackendService(name)
		if err != nil {
			return nil, err
		}
	}
	return toBackendService(gceObj)
}

// toBackendService converts a compute alpha or GA
// BackendService into our composite type.
func toBackendService(obj interface{}) (*backends.BackendService, error) {
	fmt.Println("Object ot unmarshel: ", obj)
	be := &backends.BackendService{}
	bytes, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("could not marshal object %+v to JSON: %v", obj, err)
	}
	err = json.Unmarshal(bytes, be)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling to BackendService: %v", err)
	}
	return be, nil
}

func mergeBackendSettingsWithService(b *backends.BackendService, settings map[string]interface{}) (*backends.BackendService, error) {
	s, err := json.Marshal(settings)
	if err != nil {
		return &backends.BackendService{}, err
	}

	err = json.Unmarshal([]byte(s), &b) //.Decode(&b)
	if err != nil {
		fmt.Println(err)
	}

	return b, err
}

func updateBackendService(version meta.Version, be *backends.BackendService, cloud *gce.GCECloud) error {
	if version == meta.VersionAlpha {
		alpha, err := toAlpha(be)
		if err != nil {
			return err
		}
		glog.V(3).Infof("Updating alpha backend service %v", alpha.Name)
		return cloud.UpdateAlphaGlobalBackendService(alpha)
	}
	ga, err := toGA(be)
	if err != nil {
		return err
	}
	glog.V(3).Infof("Updating ga backend service %v", ga.Name)
	return cloud.UpdateGlobalBackendService(ga)
}

// toAlpha converts our composite type into an alpha type.
// This alpha type can be used in GCE API calls.
func toAlpha(be *backends.BackendService) (*computealpha.BackendService, error) {
	bytes, err := json.Marshal(be)
	if err != nil {
		return nil, fmt.Errorf("error marshalling BackendService to JSON: %v", err)
	}
	alpha := &computealpha.BackendService{}
	err = json.Unmarshal(bytes, alpha)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling BackendService JSON to compute alpha type: %v", err)
	}
	return alpha, nil
}

// toGA converts our composite type into a GA type.
// This GA type can be used in GCE API calls.
func toGA(be *backends.BackendService) (*compute.BackendService, error) {
	bytes, err := json.Marshal(be)
	if err != nil {
		return nil, fmt.Errorf("error marshalling BackendService to JSON: %v", err)
	}
	ga := &compute.BackendService{}
	err = json.Unmarshal(bytes, ga)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling BackendService JSON to compute GA type: %v", err)
	}
	return ga, nil
}
