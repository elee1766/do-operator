/*
Copyright 2022 DigitalOcean.

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

package controllers

import (
	"context"
	"fmt"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerror "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/digitalocean/do-operator/api/v1alpha1"
	databasesv1alpha1 "github.com/digitalocean/do-operator/api/v1alpha1"
	"github.com/digitalocean/godo"
	"github.com/google/go-cmp/cmp"
)

// DatabaseReplicaReconciler reconciles a DatabaseReplica object
type DatabaseReplicaReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	GodoClient *godo.Client
}

//+kubebuilder:rbac:groups=databases.digitalocean.com,resources=databasereadonlyreplicas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=databases.digitalocean.com,resources=databasereadonlyreplicas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=databases.digitalocean.com,resources=databasereadonlyreplicas/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the replica closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DatabaseReplica object against the actual replica state, and then
// perform operations to make the replica state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *DatabaseReplicaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ll := log.FromContext(ctx)
	ll.Info("reconciling DatabaseReplica", "name", req.Name)

	var replica v1alpha1.DatabaseReplica
	err := r.Get(ctx, req.NamespacedName, &replica)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return result, nil
		}
		return result, fmt.Errorf("failed to get DatabaseReplica %s: %s", req.NamespacedName, err)
	}

	originalReplica := replica.DeepCopy()
	inDeletion := !replica.DeletionTimestamp.IsZero()

	defer func() {
		var (
			updated = false
			errs    []error
		)

		if !cmp.Equal(replica.Finalizers, originalReplica.Finalizers) {
			ll.Info("updating DatabaseReplica finalizers")
			if err := r.Patch(ctx, replica.DeepCopy(), client.MergeFrom(originalReplica)); err != nil {
				errs = append(errs, fmt.Errorf("failed to update DatabaseReplica: %s", err))
			} else {
				updated = true
			}
		}

		if diff := cmp.Diff(replica.Status, originalReplica.Status); diff != "" {
			ll.WithValues("diff", diff).Info("status diff detected")

			if err := r.Status().Patch(ctx, &replica, client.MergeFrom(originalReplica)); err != nil {
				errs = append(errs, fmt.Errorf("failed to update DatabaseReplica status: %s", err))
			} else {
				updated = true
			}
		}

		if len(errs) == 0 {
			if updated {
				ll.Info("DatabaseReplica update succeeded")
			} else {
				ll.Info("no DatabaseReplica update necessary")
			}
		}

		retErr = utilerror.NewAggregate(append([]error{retErr}, errs...))
	}()

	if inDeletion {
		ll.Info("deleting DatabaseReplica")
		result, err = r.reconcileDeleted(ctx, &replica)
	} else if replica.Status.UUID != "" {
		ll.Info("reconciling existing DatabaseReplica")
		result, err = r.reconcileExisting(ctx, &replica)
	} else {
		ll.Info("reconciling new DatabaseReplica")
		result, err = r.reconcileNew(ctx, &replica)
	}

	return ctrl.Result{}, nil
}

func (r *DatabaseReplicaReconciler) reconcileNew(ctx context.Context, replica *v1alpha1.DatabaseReplica) (ctrl.Result, error) {
	ll := log.FromContext(ctx)

	createReq := replica.Spec.ToGodoCreateRequest()
	db, _, err := r.GodoClient.Databases.CreateReplica(ctx, replica.Spec.PrimaryClusterUUID, createReq)
	if err != nil {
		ll.Error(err, "unable to create DB")
		return ctrl.Result{}, fmt.Errorf("creating DB replica: %v", err)
	}

	controllerutil.AddFinalizer(replica, finalizerName)
	replica.Status.UUID = db.ID
	replica.Status.CreatedAt = metav1.NewTime(db.CreatedAt)
	replica.Status.Status = db.Status

	err = r.ensureOwnedObjects(ctx, replica, db)
	if err != nil {
		ll.Error(err, "unable to ensure DB-related objects")
		return ctrl.Result{}, fmt.Errorf("ensuring DB-related objects: %v", err)
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *DatabaseReplicaReconciler) reconcileDeleted(ctx context.Context, replica *v1alpha1.DatabaseReplica) (ctrl.Result, error) {
	return reconcile.Result{}, nil
}

func (r *DatabaseReplicaReconciler) reconcileExisting(ctx context.Context, replica *v1alpha1.DatabaseReplica) (ctrl.Result, error) {
	return reconcile.Result{}, nil
}

func (r *DatabaseReplicaReconciler) ensureOwnedObjects(ctx context.Context, replica *v1alpha1.DatabaseReplica, db *godo.Database) error {
	objs := []client.Object{}
	if db.Connection != nil {
		objs = append(objs, connectionConfigMapForDB("-connection", replica, db.Connection))
	}
	if db.PrivateConnection != nil {
		objs = append(objs, connectionConfigMapForDB("-private-connection", replica, db.PrivateConnection))
	}

	if db.Connection != nil && db.Connection.Password != "" {
		// MongoDB doesn't return the default user password with the DB except
		// on creation. Don't update the credentials if the password is empty,
		// but create the secret if we have the password.
		objs = append(objs, credentialsSecretForDefaultDBUser(replica, db))
	}

	for _, obj := range objs {
		controllerutil.SetControllerReference(replica, obj, r.Scheme)
		if err := r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("do-operator")); err != nil {
			return fmt.Errorf("applying object %s: %s", client.ObjectKeyFromObject(obj), err)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReplicaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&databasesv1alpha1.DatabaseReplica{}).
		Complete(r)
}
