# Kan-Brewer

Schedule periodic backups in Kubernetes using [Kanister](https://kanister.io/) and Kubernetes CronJobs.

## Assumptions

You have Kanister set up and running, and you have a set of blueprints that you want to run periodically. Blueprints
apply to either whole namespace (e.g. `kanister-mysql-blueprint`) or to a specific PVC (e.g. `kanister-pvc-blueprint`).

## How to 

1. Annotate the namespaces and PVCs that you want to back up with the `kan-brewer.haim.dev/kanister-blueprints` annotation.
It should list all the blueprints that you want to use on that namespace or PVC.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
  annotations:
    kan-brewer.haim.dev/kanister-blueprints: backup-pbs-postgresql,backup-pbs-mongodb,backup-pbs-mariadb
```

2. Set up a CronJob to run `kan-brewer` container periodically. The CronJob should use a service account that has
permissions to list namespaces and PVCs, and to create and delete Kanister action sets.

## What will it do?

### Creates new action sets

`kan-brewer` will create actionsets that reference the blueprints you specified in the annotation, 
and the PVC or Namespace object itself. For example:

```yaml
apiVersion: cr.kanister.io/v1alpha1
kind: ActionSet
metadata:
  generateName: auto-backup-pbs-mariadb-test-
  labels:
    app.kubernetes.io/managed-by: kan-brewer
  namespace: kanister
spec:
  actions:
  - blueprint: backup-pbs-mariadb
    name: backup
    object:
      kind: Namespace
      name: test
```
### Deletes old action sets

`kan-brewer` will delete old action sets: 
* Those that were created by it (i.e. have the `app.kubernetes.io/managed-by: kan-brewer`),
* Successful action sets only (i.e. those that have `state: complete`).
* It will keep 3 latest ones (configurable).

## TODO

* [ ] Add support for Kanister profiles and options
* [ ] Create a Helm chart that will install `kan-brewer` and set up a CronJob
