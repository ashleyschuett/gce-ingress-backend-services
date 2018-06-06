package gce

import (
	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

func New() *gce.GCECloud {
	return app.NewGCEClient()
}
