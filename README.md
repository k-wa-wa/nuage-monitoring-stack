
```bash
kustomize build --enable-helm k8s/overlays/local | kubectl -n nuage-monitoring-stack apply --server-side -f -
```
