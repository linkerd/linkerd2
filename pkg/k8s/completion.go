package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CommandCompletion generates CLI suggestions from resources in a given cluster
// It uses a list of arguments and a substring from the CLI to filter suggestions.
type CommandCompletion struct {
	k8sAPI    *KubernetesAPI
	namespace string
}

// NewCommandCompletion creates a command completion module
func NewCommandCompletion(
	k8sAPI *KubernetesAPI,
	namespace string,
) *CommandCompletion {
	return &CommandCompletion{
		k8sAPI:    k8sAPI,
		namespace: namespace,
	}
}

// Complete accepts a list of arguments and a substring to generate CLI suggestions.
// `args` represent a list of arguments a user has already entered in the CLI. These
// arguments are used for determining what resource type we'd like to receive
// suggestions for as well as a list of resources names that have already provided.
// `toComplete` represents the string prefix of a resource name that we'd like to
// use to search for suggestions
//
// If `args` is empty, send back a list of all resource types support by the CLI.
// If `args` has at least one or more items, assume that the first item in `args`
// is the resource type we are trying to get suggestions for e.g. Deployment, StatefulSets.
//
// Complete is generic enough so that it can find suggestions for any type of resource
// in a Kubernetes cluster. It does this by first querying what GroupVersion a resource
// belongs to and then does a dynamic `List` query to get resources under that GroupVersion
func (c *CommandCompletion) Complete(args []string, toComplete string) ([]string, error) {
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	suggestions := []string{}
	if len(args) == 0 && toComplete == "" {
		return CompletionResourceTypes, nil
	}

	if len(args) == 0 && toComplete != "" {
		for _, t := range CompletionResourceTypes {
			if strings.HasPrefix(t, toComplete) {
				suggestions = append(suggestions, t)
			}
		}
		return suggestions, nil
	}

	// Similar to kubectl, we don't provide resource completion
	// when the resource provided is in format <kind>/<resourceName>
	if strings.Contains(args[0], "/") {
		return []string{}, nil
	}

	resType, err := CanonicalResourceNameFromFriendlyName(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid resource name", args)
	}

	// if we are looking for namespace suggestions clear namespace selector
	if resType == "namespace" {
		c.namespace = ""
	}

	gvr, err := c.getGroupVersionKindForResource(resType)
	if err != nil {
		return nil, err
	}

	uList, err := c.k8sAPI.DynamicClient.
		Resource(*gvr).
		Namespace(c.namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	otherResources := args[1:]
	for _, u := range uList.Items {
		name := u.GetName()

		// Filter out the list of resource items returned from k8s API to
		// only include items that have the prefix `toComplete` and items
		// that aren't in the list of resources already provided in the
		// list of arguments.
		//
		// This is useful so that we avoid duplicate suggestions if they
		// are already in the list of args
		if strings.HasPrefix(name, toComplete) &&
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

	// find the plural name to ensure the resource we are searching for is not a subresource
	// i.e. deployment/scale
	pluralResourceName, err := PluralResourceNameFromFriendlyName(resourceName)
	if err != nil {
		return nil, fmt.Errorf("%s not a valid resource name", resourceName)
	}

	gvr, err := findGroupVersionResource(resourceName, pluralResourceName, apiResourceList)
	if err != nil {
		return nil, fmt.Errorf("could not find GroupVersionResource for %s", resourceName)
	}

	return gvr, nil
}

func findGroupVersionResource(singularName string, pluralName string, apiResourceList []*metav1.APIResourceList) (*schema.GroupVersionResource, error) {
	err := fmt.Errorf("could not find the requested resource")
	for _, res := range apiResourceList {
		for _, r := range res.APIResources {

			// Make sure we get a resource type where its Kind matches the
			// singularName passed into this function and its Name (which is always
			// the pluralName of an api resource) matches the pluralName passed
			// into this function. Skip further processing of this APIResource
			// if this is not the case.
			if strings.ToLower(r.Kind) != singularName || r.Name != pluralName {
				continue
			}

			gv := strings.Split(res.GroupVersion, "/")

			if len(gv) == 1 && gv[0] == "v1" {
				return &schema.GroupVersionResource{
					Version:  gv[0],
					Resource: r.Name,
				}, nil
			}

			if len(gv) != 2 {
				return nil, err
			}

			return &schema.GroupVersionResource{
				Group:    gv[0],
				Version:  gv[1],
				Resource: r.Name,
			}, nil
		}
	}

	return nil, err
}

func containsResource(resource string, otherResources []string) bool {
	for _, r := range otherResources {
		if r == resource {
			return true
		}
	}

	return false
}
