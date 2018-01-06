package operator

import (
	"errors"
	"reflect"

	"github.com/appscode/go/log"
	"github.com/appscode/kutil/meta"
	tapi "github.com/k8sdb/apimachinery/apis/kubedb/v1alpha1"
	kutil "github.com/k8sdb/apimachinery/client/typed/kubedb/v1alpha1/util"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	rt "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// Blocks caller. Intended to be called as a Go routine.
func (op *Operator) WatchSnapshots() {
	if !meta.IsPreferredAPIResource(op.KubeClient, tapi.SchemeGroupVersion.String(), tapi.ResourceKindSnapshot) {
		log.Warningf("Skipping watching non-preferred GroupVersion:%s Kind:%s", tapi.SchemeGroupVersion.String(), tapi.ResourceKindSnapshot)
		return
	}

	defer rt.HandleCrash()

	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return op.KubeDBClient.Snapshots(core.NamespaceAll).List(metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return op.KubeDBClient.Snapshots(core.NamespaceAll).Watch(metav1.ListOptions{})
		},
	}
	_, ctrl := cache.NewInformer(lw,
		&tapi.Snapshot{},
		op.Opt.ResyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if res, ok := obj.(*tapi.Snapshot); ok {
					log.Infof("Snapshot %s@%s added", res.Name, res.Namespace)
					kutil.AssignTypeKind(res)

					if op.Config.APIServer.EnableSearchIndex {
						if err := op.SearchIndex.HandleAdd(obj); err != nil {
							log.Errorln(err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if res, ok := obj.(*tapi.Snapshot); ok {
					log.Infof("Snapshot %s@%s deleted", res.Name, res.Namespace)
					kutil.AssignTypeKind(res)

					if op.Config.APIServer.EnableSearchIndex {
						if err := op.SearchIndex.HandleDelete(obj); err != nil {
							log.Errorln(err)
						}
					}
					if op.TrashCan != nil {
						op.TrashCan.Delete(res.TypeMeta, res.ObjectMeta, obj)
					}
				}
			},
			UpdateFunc: func(old, new interface{}) {
				oldRes, ok := old.(*tapi.Snapshot)
				if !ok {
					log.Errorln(errors.New("invalid Snapshot object"))
					return
				}
				newRes, ok := new.(*tapi.Snapshot)
				if !ok {
					log.Errorln(errors.New("invalid Snapshot object"))
					return
				}
				kutil.AssignTypeKind(oldRes)
				kutil.AssignTypeKind(newRes)

				if op.Config.APIServer.EnableSearchIndex {
					op.SearchIndex.HandleUpdate(old, new)
				}
				if op.TrashCan != nil && op.Config.RecycleBin.HandleUpdates {
					if !reflect.DeepEqual(oldRes.Labels, newRes.Labels) ||
						!reflect.DeepEqual(oldRes.Annotations, newRes.Annotations) ||
						!reflect.DeepEqual(oldRes.Spec, newRes.Spec) {
						op.TrashCan.Update(newRes.TypeMeta, newRes.ObjectMeta, old, new)
					}
				}
			},
		},
	)
	ctrl.Run(wait.NeverStop)
}