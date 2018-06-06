package annotations

import (
	"fmt"

	"k8s.io/ingress-gce/pkg/annotations"
)

const GooglePrefix = "cloud.google.com"

var (
	Backends = fmt.Sprintf("%v/backends", annotations.StatusPrefix)

	URLMap = fmt.Sprintf("%v/url-map", annotations.StatusPrefix)
)

var (
	ServiceSettings = fmt.Sprintf("%v/service-settings", GooglePrefix)
)
