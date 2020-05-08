/*
 *
 * Copyright 2020 The KubeSphere Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 * /
 */
package openpitrix

import (
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/klog"
	"kubesphere.io/kubesphere/pkg/models"
	"kubesphere.io/kubesphere/pkg/server/params"
	"kubesphere.io/kubesphere/pkg/simple/client/openpitrix"
	"openpitrix.io/openpitrix/pkg/pb"
	"openpitrix.io/openpitrix/pkg/util/pbutil"
)

type ReleaseInterface interface {
	ListReleases(conditions *params.Conditions, limit, offset int) (*models.PageableResponse, error)
	DescribeRelease(releaseName, namespace, runtimeId string) (*Release, error)
	CreateRelease(namespace string, request CreateReleaseRequest) error
	UpgradeRelease(request CreateReleaseRequest) error
	DeleteRelease(runtimeId, releaseName string) error
}

type releaseOperator struct {
	informers informers.SharedInformerFactory
	opClient  openpitrix.Client
}

func newReleaseOperator(informers informers.SharedInformerFactory, opClient openpitrix.Client) ReleaseInterface {
	return &releaseOperator{informers: informers, opClient: opClient}
}

type Release struct {
	ReleaseName      string            `json:"name" description:"release name"`
	WorkLoads *workLoads        `json:"workloads,omitempty" description:"release workloads"`
	Services  []v1.Service      `json:"services,omitempty" description:"release services"`
	Ingresses []v1beta1.Ingress `json:"ingresses,omitempty" description:"release ingresses"`
}

func (c *releaseOperator) ListReleases(conditions *params.Conditions, limit, offset int) (*models.PageableResponse, error) {
	listReleasesRequest := &pb.ListReleasesRequest{
		Limit:  uint32(limit),
		Offset: uint32(offset)}
	if runtimeId := conditions.Match[RuntimeId]; runtimeId != "" {
		listReleasesRequest.RuntimeId = pbutil.ToProtoString(runtimeId)
	}
	if status := conditions.Match[Status]; status != "" {
		listReleasesRequest.Status = pbutil.ToProtoString(status)
	}
	resp, err := c.opClient.ListReleases(openpitrix.SystemContext(), listReleasesRequest)
	if err != nil {
		klog.Errorln(err)
		return nil, err
	}
	result := models.PageableResponse{TotalCount: int(resp.TotalCount)}
	result.Items = make([]interface{}, 0)
	for _, r := range resp.ReleaseSet {
		result.Items = append(result.Items, r)
	}

	return &result, nil
}

func (c *releaseOperator) CreateRelease(namespace string, request CreateReleaseRequest) error {
	createRelease := &pb.CreateReleaseRequest{
		AppId:       pbutil.ToProtoString(request.AppId),
		VersionId:   pbutil.ToProtoString(request.VersionId),
		RuntimeId:   pbutil.ToProtoString(request.RuntimeId),
		ReleaseName: pbutil.ToProtoString(request.ReleaseName),
		Namespace:   pbutil.ToProtoString(namespace),
	}

	_, err := c.opClient.CreateRelease(openpitrix.SystemContext(), createRelease)
	if err != nil {
		klog.Errorln(err)
		return err
	}
	return nil
}

func (c *releaseOperator) UpgradeRelease(request CreateReleaseRequest) error {
	upgradeRelease := &pb.UpgradeReleaseRequest{}

	_, err := c.opClient.UpgradeRelease(openpitrix.SystemContext(), upgradeRelease)
	if err != nil {
		klog.Errorln(err)
		return err
	}
	return nil
}

func (c *releaseOperator) DeleteRelease(runtimeId, releaseName string) error {
	deleteReleaseRequest := &pb.DeleteReleaseRequest{
		RuntimeId:   pbutil.ToProtoString(runtimeId),
		ReleaseName: pbutil.ToProtoString(releaseName),
	}

	_, err := c.opClient.DeleteRelease(openpitrix.SystemContext(), deleteReleaseRequest)
	return err
}

func (c *releaseOperator) DescribeRelease(releaseName, namespace, runtimeId string) (*Release, error) {
	describeRelease := &pb.DescribeReleaseRequest{
		RuntimeId:   pbutil.ToProtoString(runtimeId),
		ReleaseName: pbutil.ToProtoString(releaseName),
	}
	describeReleaseResp, err := c.opClient.DescribeRelease(openpitrix.SystemContext(), describeRelease)
	if err != nil{
		return nil,err
	}
	roles := describeReleaseResp.GetWorkload()
	workLoad, err := c.getWorkLoads(namespace, roles)
	workLoadLabels := c.getLabels(namespace, workLoad)
	svcs := c.getSvcs(namespace, workLoadLabels)
	ing := c.getIng(namespace, svcs)

	release := &Release{
		ReleaseName:releaseName,
		WorkLoads:workLoad,
		Services:svcs,
		Ingresses:ing,
	}

	return release,nil
}

func (c *releaseOperator) getWorkLoads(namespace string, roles map[string]string) (*workLoads, error) {

	var works workLoads
	for workLoadName, v := range roles {
		if len(workLoadName) > 0 {
			if workLoadName == openpitrix.Deployment {
				item, err := c.informers.Apps().V1().Deployments().Lister().Deployments(namespace).Get(v)

				if err != nil {
					// app not ready
					if errors.IsNotFound(err) {
						continue
					}
					klog.Errorln(err)
					return nil, err
				}

				works.Deployments = append(works.Deployments, *item)
				continue
			}

			if workLoadName == openpitrix.DaemonSet {
				item, err := c.informers.Apps().V1().DaemonSets().Lister().DaemonSets(namespace).Get(v)
				if err != nil {
					// app not ready
					if errors.IsNotFound(err) {
						continue
					}
					klog.Errorln(err)
					return nil, err
				}
				works.Daemonsets = append(works.Daemonsets, *item)
				continue
			}

			if workLoadName == openpitrix.StatefulSet {
				item, err := c.informers.Apps().V1().StatefulSets().Lister().StatefulSets(namespace).Get(v)
				if err != nil {
					// app not ready
					if errors.IsNotFound(err) {
						continue
					}
					klog.Errorln(err)
					return nil, err
				}
				works.Statefulsets = append(works.Statefulsets, *item)
				continue
			}
		}
	}
	return &works, nil
}

func (c *releaseOperator) getLabels(namespace string, workloads *workLoads) *[]map[string]string {

	var workloadLabels []map[string]string
	if workloads == nil {
		return nil
	}

	for _, workload := range workloads.Deployments {
		deploy, err := c.informers.Apps().V1().Deployments().Lister().Deployments(namespace).Get(workload.Name)
		if errors.IsNotFound(err) {
			continue
		}
		workloadLabels = append(workloadLabels, deploy.Labels)
	}

	for _, workload := range workloads.Daemonsets {
		daemonset, err := c.informers.Apps().V1().DaemonSets().Lister().DaemonSets(namespace).Get(workload.Name)
		if errors.IsNotFound(err) {
			continue
		}
		workloadLabels = append(workloadLabels, daemonset.Labels)
	}

	for _, workload := range workloads.Statefulsets {
		statefulset, err := c.informers.Apps().V1().StatefulSets().Lister().StatefulSets(namespace).Get(workload.Name)
		if errors.IsNotFound(err) {
			continue
		}
		workloadLabels = append(workloadLabels, statefulset.Labels)
	}

	return &workloadLabels
}

func (c *releaseOperator) isExist(svcs []v1.Service, svc *v1.Service) bool {
	for _, item := range svcs {
		if item.Name == svc.Name && item.Namespace == svc.Namespace {
			return true
		}
	}
	return false
}

func (c *releaseOperator) getSvcs(namespace string, workLoadLabels *[]map[string]string) []v1.Service {
	if len(*workLoadLabels) == 0 {
		return nil
	}
	var services []v1.Service
	for _, label := range *workLoadLabels {
		labelSelector := labels.Set(label).AsSelector()
		svcs, err := c.informers.Core().V1().Services().Lister().Services(namespace).List(labelSelector)
		if err != nil {
			klog.Errorf("get app's svc failed, reason: %v", err)
		}
		for _, item := range svcs {
			if !c.isExist(services, item) {
				services = append(services, *item)
			}
		}
	}

	return services
}

func (c *releaseOperator) getIng(namespace string, services []v1.Service) []v1beta1.Ingress {
	if services == nil {
		return nil
	}

	var ings []v1beta1.Ingress
	for _, svc := range services {
		ingresses, err := c.informers.Extensions().V1beta1().Ingresses().Lister().Ingresses(namespace).List(labels.Everything())
		if err != nil {
			klog.Error(err)
			return ings
		}

		for _, ingress := range ingresses {
			if ingress.Spec.Backend.ServiceName != svc.Name {
				continue
			}

			exist := false
			var tmpRules []v1beta1.IngressRule
			for _, rule := range ingress.Spec.Rules {
				for _, p := range rule.HTTP.Paths {
					if p.Backend.ServiceName == svc.Name {
						exist = true
						tmpRules = append(tmpRules, rule)
					}
				}
			}

			if exist {
				ing := v1beta1.Ingress{}
				ing.Name = ingress.Name
				ing.Spec.Rules = tmpRules
				ings = append(ings, ing)
			}
		}
	}

	return ings
}
