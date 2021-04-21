package k8s

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type CommandCompletion struct {
	k8sAPI                  *KubernetesAPI
	namespace               string
	allNamespaces           bool
}

func NewCommandCompletion(
	k8sAPI *KubernetesAPI,
	namespace string,
) *CommandCompletion {
	return &CommandCompletion{
		k8sAPI:    k8sAPI,
		namespace: namespace,
	}
}

func (c *CommandCompletion) WithAllNamespaces() *CommandCompletion {
	c.allNamespaces = true
	return c
}

func (c *CommandCompletion) Complete(args []string, toComplete string) ([]string, error) {
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	suggestions := []string{}
	autoCmpRegexp := regexp.MustCompile(fmt.Sprintf("^%s.*", toComplete))
	if len(args) == 0 && toComplete == "" {
		return StatAllResourceTypes, nil
	}

	if len(args) == 0 && toComplete != "" {
		for _, t := range StatAllResourceTypes {
			if autoCmpRegexp.MatchString(t) {
				suggestions = append(suggestions, t)
			}
		}
		return suggestions, nil
	}

	resType, err := CanonicalResourceNameFromFriendlyName(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s not a valid resource name", args)
	}

	var namespace string
	if c.namespace != "" {
		namespace = c.namespace
	} else if c.namespace == "" && !c.allNamespaces {
		namespace = "default"
	}

	// if we are looking for namespace suggestions clear namespace selector
	if resType == "namespace" {
		namespace = ""
	}

	// Similar to kubectl, we don't provide resource completion
	// when the resource provided is in format <kind>/<resourceName>
	if strings.Contains(args[0], "/") {
		return []string{}, nil
	}

	gvr, err := c.getGroupVersionKindForResource(resType)
	if err != nil {
		return nil, err
	}

	uList, err := c.k8sAPI.DynamicClient.
		Resource(*gvr).
		Namespace(namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	otherResources := args[1:]
	for _, u := range uList.Items {
		name := u.GetName()
		if autoCmpRegexp.MatchString(name) &&
			!containsResource(name, otherResources) {
			suggestions = append(suggestions, name)
		}
	}

	return suggestions, nil
}

func (c *CommandCompletion) getGroupVersionKindForResource(resourceName string) (*schema.GroupVersionResource, error) {
	_, apiResourceList, err := c.k8sAPI.Discovery().ServerGroupsAndResources()
	if err != nil {
		return nil, err
	}

	var gvr *schema.GroupVersionResource
	for _, res := range apiResourceList {
		for _, r := range res.APIResources {
			if strings.ToLower(r.Kind) == resourceName {
				gv := strings.Split(res.GroupVersion, "/")

				if len(gv) == 1 && gv[0] == "v1" {
					gvr = &schema.GroupVersionResource{
						Version:  gv[0],
						Resource: r.Name,
					}
					break
				}

				if len(gv) != 2 {
					return nil, fmt.Errorf("could not find the requested resource")
				}

				gvr = &schema.GroupVersionResource{
					Group:    gv[0],
					Version:  gv[1],
					Resource: r.Name,
				}
			}
		}

		if gvr != nil {
			break
		}
	}

	if gvr == nil {
		return nil, fmt.Errorf("could not find GroupVersionResource for %s", resourceName)
	}

	return gvr, nil
}

func containsResource(resource string, otherResources []string) bool {
	for _, r := range otherResources {
		if r == resource {
			return true
		}
	}

	return false
}
