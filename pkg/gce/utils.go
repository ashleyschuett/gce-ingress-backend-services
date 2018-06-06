package gce

import (
	extensions "k8s.io/api/extensions/v1beta1"
	flags "k8s.io/ingress-gce/pkg/flags"

	"k8s.io/ingress-gce/pkg/annotations"
)

// Both isGCEIngress && isGCEMultiClusterIngress are directly copied from
// gce-ingress controller/utils.go, they are unexported so I was unabled
// to use them from the package but wanted the logic to match the gce-ingress
// controller

// isGCEIngress returns true if the Ingress matches the class managed by this
// controller.
func IsGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	if flags.F.IngressClass == "" {
		return class == "" || class == annotations.GceIngressClass
	}
	return class == flags.F.IngressClass
}

// isGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func IsGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceMultiIngressClass
}
