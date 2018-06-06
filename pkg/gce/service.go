package gce

import (
	"fmt"

	extensions "k8s.io/api/extensions/v1beta1"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/controller/errors"
	"k8s.io/ingress-gce/pkg/utils"

	corelistersv1 "k8s.io/client-go/listers/core/v1"
)

func getServiceID(ing extensions.IngressBackend, namespace string) utils.ServicePortID {
	return utils.BackendToServicePortID(ing, namespace)
}

func getServiceProtocol(sa corelistersv1.ServiceLister, id utils.ServicePortID) (*utils.ServicePort, error) {
	svc, err := sa.Services(id.Service.Namespace).Get(id.Service.Name)
	if err != nil {
		return nil, fmt.Errorf("error retrieving service %q: %v", id.Service, err)
	}

	appProtocols, err := annotations.FromService(svc).ApplicationProtocols()
	if err != nil {
		return nil, errors.ErrSvcAppProtosParsing{Svc: svc, Err: err}
	}

	var port *corev1.ServicePort
PortLoop:
	for _, p := range svc.Spec.Ports {
		np := p
		switch id.Port.Type {
		case intstr.Int:
			if p.Port == id.Port.IntVal {
				port = &np
				break PortLoop
			}
		default:
			if p.Name == id.Port.StrVal {
				port = &np
				break PortLoop
			}
		}
	}

	if port == nil {
		return nil, errors.ErrSvcPortNotFound{ServicePortID: id}
	}

	proto := annotations.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		proto = annotations.AppProtocol(protoStr)
	}

	return &utils.ServicePort{
		ID:       id,
		NodePort: int64(port.NodePort),
		Protocol: proto,
	}, nil
}
