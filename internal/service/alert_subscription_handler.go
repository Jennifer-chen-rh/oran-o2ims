/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	jsoniter "github.com/json-iterator/go"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// DeploymentManagerHandlerBuilder contains the data and logic needed to create a new deployment
// manager collection handler. Don't create instances of this type directly, use the
// NewDeploymentManagerHandler function instead.
type AlertSubscriptionHandlerBuilder struct {
	logger         *slog.Logger
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	cloudID        string
	extensions     []string
}

// AlertSubscriptionHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewAlertSubscriptionHandler function
// instead.
type AlertSubscriptionHandler struct {
	logger                   *slog.Logger
	loggingWrapper           func(http.RoundTripper) http.RoundTripper
	cloudID                  string
	extensions               []string
	jsonAPI                  jsoniter.API
	selectorEvaluator        *search.SelectorEvaluator
	jqTool                   *jq.Tool
	subscritionMapMemoryLock *sync.Mutex
	subscriptionMap          map[string]data.Object
}

// NewAlertSubscriptionHandler creates a builder that can then be used to configure and create a
// handler for the collection of deployment managers.
func NewAlertSubscriptionHandler() *AlertSubscriptionHandlerBuilder {
	return &AlertSubscriptionHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *AlertSubscriptionHandlerBuilder) SetLogger(
	value *slog.Logger) *AlertSubscriptionHandlerBuilder {
	b.logger = value
	return b
}

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *AlertSubscriptionHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *AlertSubscriptionHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *AlertSubscriptionHandlerBuilder) SetCloudID(
	value string) *AlertSubscriptionHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *AlertSubscriptionHandlerBuilder) SetExtensions(
	values ...string) *AlertSubscriptionHandlerBuilder {
	b.extensions = values
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *AlertSubscriptionHandlerBuilder) Build() (
	result *AlertSubscriptionHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.cloudID == "" {
		err = errors.New("cloud identifier is mandatory")
		return
	}

	// Prepare the JSON iterator API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create the filter expression evaluator:
	pathEvaluator, err := search.NewPathEvaluator().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}
	selectorEvaluator, err := search.NewSelectorEvaluator().
		SetLogger(b.logger).
		SetPathEvaluator(pathEvaluator.Evaluate).
		Build()
	if err != nil {
		return
	}

	// Create the jq tool:
	jqTool, err := jq.NewTool().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Check that extensions are at least syntactically valid:
	for _, extension := range b.extensions {
		_, err = jqTool.Compile(extension)
		if err != nil {
			return
		}
	}

	// Create and populate the object:
	result = &AlertSubscriptionHandler{
		logger:                   b.logger,
		loggingWrapper:           b.loggingWrapper,
		cloudID:                  b.cloudID,
		extensions:               slices.Clone(b.extensions),
		selectorEvaluator:        selectorEvaluator,
		jsonAPI:                  jsonAPI,
		jqTool:                   jqTool,
		subscritionMapMemoryLock: &sync.Mutex{},
		subscriptionMap:          map[string]data.Object{},
	}

	b.logger.Debug(
		"AlertSubscriptionHandler build:",
		"CloudID", b.cloudID,
	)

	return
}

// List is the implementation of the collection handler interface.
func (h *AlertSubscriptionHandler) List(ctx context.Context,
	request *ListRequest) (response *ListResponse, err error) {
	// Create the stream that will fetch the items:
	var items data.Stream

	// Transform the items into what we need:
	items = data.Map(items, h.mapItem)

	// Select only the items that satisfy the filter:
	if request.Selector != nil {
		items = data.Select(
			items,
			func(ctx context.Context, item data.Object) (result bool, err error) {
				result, err = h.selectorEvaluator.Evaluate(ctx, request.Selector, item)
				return
			},
		)
	}

	// Return the result:
	response = &ListResponse{
		Items: items,
	}
	return
}

func (h *AlertSubscriptionHandler) RetrieveSubscriptionMapValue(
	request *GetRequest) (item data.Object, err error) {
	h.subscritionMapMemoryLock.Lock()
	defer h.subscritionMapMemoryLock.Unlock()
	item, ok := h.subscriptionMap[request.Variables[0]]
	if !ok {
		err = ErrNotFound
		return
	}
	return
}

// Get is the implementation of the object handler interface.
func (h *AlertSubscriptionHandler) Get(ctx context.Context,
	request *GetRequest) (response *GetResponse, err error) {

	h.logger.Debug(
		"AlertSubscriptionHandler Get:",
		"id", request.Variables[0],
	)
	item, err := h.RetrieveSubscriptionMapValue(request)

	if err != nil {
		return
	}

	// Transform the object into what we need:
	item, err = h.mapItem(ctx, item)
	if err != nil {
		return
	}

	// Return the result:
	response = &GetResponse{
		Object: item,
	}
	return
}

func (h *AlertSubscriptionHandler) fetchItem(ctx context.Context,
	id string) (result data.Object, err error) {
	// Currently the ACM global hub API that we use doesn't have a specific endpoint for
	// retrieving a specific object, instead of that we need to fetch a list filtering with a
	// label selector.
	/* query := neturl.Values{}
	query.Set("labelSelector", fmt.Sprintf("clusterID=%s", id))
	query.Set("limit", "1") */

	request := &GetRequest{Variables: []string{id}}
	response, err := h.Get(ctx, request)
	if err != nil {
		return
	}
	result = response.Object
	return
}

// items needs to be investigation
func (h *AlertSubscriptionHandler) fetchItems(
	ctx context.Context) (result data.Stream, err error) {
	request := &GetRequest{Variables: []string{}}
	//response, err := h.Get(ctx, request)
	response, err := h.Get(ctx, request)
	if err != nil {
		return
	}
	h.logger.Debug(
		"AlertSubscriptionHandler fetchItems:",
		"Object", response.Object,
	)
	//result = data.Stream(response.Object)
	result = data.Stream(nil)
	return
}

func (h *AlertSubscriptionHandler) mapItem(ctx context.Context,
	input data.Object) (output data.Object, err error) {

	return input, nil
}
