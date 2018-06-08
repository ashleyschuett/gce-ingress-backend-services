# gce-ingress-backend-service
Kubernetes controller that watches GCE ingresses and sets custom settings on the backend service

# Info
This controller is an add on to [GCE ingress](https://github.com/kubernetes/ingress-gce). It adds the functionality to control settings of a backend service on a gce loadbalancer from
the ingress annotation. This idea originated in this [ticket](https://github.com/kubernetes/ingress-gce/issues/28) on github and much of the functionality in this repo is derived from it.

### What does it do?
Allows you to customize services sitting behind your loadbalancer without
leaving kubectl or your kubernetes cluster.

### What does it not do?
Reconcile your backend service with the Ingress resource. Meaning, that if a user
goes into the GCE console and modifies a setting, that loadbalancer will persist that change.
It will only update the service settings back to what is specified in the annotation in
the event of an ingress update.

# Usage
A complete deployment example can be can be found in [/manifests](https://github.com/ashleyschuett/gce-ingress-backend-services/tree/master/manifests). To deploy
the controller you just need to create the deployment.

```yaml
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  namespace: kube-system
  name: gce-backend-service-ingress-controller
  labels:
    app: gce-backend-service-ingress-controller
spec:
  selector:
    matchLabels:
      app: gce-backend-service-ingress-controller
  template:
    metadata:
      labels:
        app: gce-backend-service-ingress-controller
    spec:
      containers:
        - name: gce-backend-service-ingress-controller
          image: ashleyschuett/gce-ingress-backend-services:v0.0.2
          imagePullPolicy: IfNotPresent
```



# Ingress Example
``` yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: test
  annotations:
     cloud.google.com/service-settings: |
       {
           "*": {
              "timeoutSec": 321
           },
          "foo.bar.com/foo": {
               "timeoutSec": 123,
               "iap": {
                   "enabled": true,
                   "oauth2ClientId": "....",
                   "oauth2ClientSecret":"..."
                }
           },
          "foo.bar.com/bar/*":{
               "enableCDN": true
          }
       }
spec:
  backend:
    serviceName: s0
    servicePort: 80
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /foo
        backend:
          serviceName: s1
          servicePort: 80
      - path: /bar/*
        backend:
          serviceName: s2
          servicePort: 80
```

# Format
To specify settings for an ingress backend you will need to specify a key as
the host path with the value being an object that is in the format of a compute
backend service. If the backend service is [alpha](https://godoc.org/google.golang.org/api/compute/v0.alpha) use the alpha BackendService type spec to base your configuration on. Otherwise, if it is ga (any service not using http2 protocol) use the
BackendService spec from [compute](https://github.com/ashleyschuett/gce-ingress-backend-services.git).

The example below would update a backend service associated with a Host path rule
with the host `foo.bar.com` and a path of `/bar/*`

```json
{
    "foo.bar.com/bar/*": {
        "enableCDN": true
    }
}
```


The only exception to this annotation format is for the default backend service. It
is instead keyed under `*`. This would match the backend service that is specified under the ingress
resource `Spec.Backend`.

```json
{
    "*": {
        "timeoutSec": 321
    }
}
