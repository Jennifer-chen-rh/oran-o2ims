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
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/persiststorage"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// AlarmNotificationManagerHandlerBuilder contains the data and logic needed to create a new alarm notification
// collection handler. Don't create instances of this type directly, use the
// NewAlarmNotificationHandler function instead.
type alarmNotificationHandlerBuilder struct {
	logger         *slog.Logger
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	cloudID        string
	extensions     []string
	kubeClient     *k8s.Client
}

// key string of uuid
type alarmSubIdSet map[string]struct{}

// expand for future filter index
//type StringSet map[string]struct{}

var filter_include_strings = [2]string{
	"eq",
	"in",
}
var filter_exclude_strings = [2]string{
	"neq",
	"nin",
}

const star = "*"

/*
type filterIndexData struct {
	filterIncludeMap map[string]StringSet
	filterExcludeMap map[string]StringSet

}*/

// alarmNotificationHander knows how to respond to requests to list deployment managers.
// Don't create instances of this type directly, use the NewAlarmNotificationHandler function
// instead.
type alarmNotificationHandler struct {
	logger            *slog.Logger
	loggingWrapper    func(http.RoundTripper) http.RoundTripper
	cloudID           string
	extensions        []string
	jsonAPI           jsoniter.API
	selectorEvaluator *search.SelectorEvaluator
	jqTool            *jq.Tool

	//structures for notification
	subscriptionMapMemoryLock *sync.RWMutex
	subscriptionMap           *map[string]data.Object
	persistStore              *persiststorage.KubeConfigMapStore
	subscriptionSearcher      *alarmSubscriptionSearcher
	httpClient                http.Client
	//filter index structures still same semaphone
	//subscriptionIdSet         alarmSubIdSet
	//filterSubscriptionMap     map[string]alarmSubIdSet
}

// NewAlarmNotificationHandler creates a builder that can then be used to configure and create a
// handler for the collection of deployment managers.
func NewAlarmNotificationHandler() *alarmNotificationHandlerBuilder {
	return &alarmNotificationHandlerBuilder{}
}

// SetLogger sets the logger that the handler will use to write to the log. This is mandatory.
func (b *alarmNotificationHandlerBuilder) SetLogger(
	value *slog.Logger) *alarmNotificationHandlerBuilder {
	b.logger = value
	return b
}

// SetLoggingWrapper sets the wrapper that will be used to configure logging for the HTTP clients
// used to connect to other servers, including the backend server. This is optional.
func (b *alarmNotificationHandlerBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *alarmNotificationHandlerBuilder {
	b.loggingWrapper = value
	return b
}

// SetCloudID sets the identifier of the O-Cloud of this handler. This is mandatory.
func (b *alarmNotificationHandlerBuilder) SetCloudID(
	value string) *alarmNotificationHandlerBuilder {
	b.cloudID = value
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *alarmNotificationHandlerBuilder) SetExtensions(
	values ...string) *alarmNotificationHandlerBuilder {
	b.extensions = values
	return b
}

// SetExtensions sets the fields that will be added to the extensions.
func (b *alarmNotificationHandlerBuilder) SetKubeClient(
	kubeClient *k8s.Client) *alarmNotificationHandlerBuilder {
	b.kubeClient = kubeClient
	return b
}

// Build uses the data stored in the builder to create anad configure a new handler.
func (b *alarmNotificationHandlerBuilder) Build(ctx context.Context) (
	result *alarmNotificationHandler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.cloudID == "" {
		err = errors.New("cloud identifier is mandatory")
		return
	}

	if b.kubeClient == nil {
		err = errors.New("kubeClient is mandatory")
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

	alarmSubscriptionSearcher := newAlarmSubscriptionSearcher()
	alarmSubscriptionSearcher.SetLogger(b.logger).build()

	// create persist storeage option
	persistStore := persiststorage.NewKubeConfigMapStore().
		SetNameSpace(TestNamespace).
		SetName(TestConfigmapName).
		SetFieldOwnder(FieldOwner).
		SetJsonAPI(&jsonAPI).
		SetClient(b.kubeClient)

	// http client to send out notification
	// use default cfg 1st
	httpClient := http.Client{}

	// Create and populate the object:
	result = &alarmNotificationHandler{
		logger:                    b.logger,
		loggingWrapper:            b.loggingWrapper,
		cloudID:                   b.cloudID,
		extensions:                slices.Clone(b.extensions),
		selectorEvaluator:         selectorEvaluator,
		jsonAPI:                   jsonAPI,
		jqTool:                    jqTool,
		subscriptionMapMemoryLock: &sync.RWMutex{},
		subscriptionMap:           &map[string]data.Object{},
		//subscriptionIdSet:         alarmSubIdSet{},
		//filterSubscriptionMap:     map[string]alarmSubIdSet{},
		persistStore:         persistStore,
		subscriptionSearcher: alarmSubscriptionSearcher,
		httpClient:           httpClient,
	}

	b.logger.Debug(
		"alarmNotificationHandler build:",
		"CloudID", b.cloudID,
	)

	err = result.recoveryFromPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"alarmNotificationHandler failed to recovery from persistStore ", err,
		)
	}

	err = result.watchPersistStore(ctx)
	if err != nil {
		b.logger.Error(
			"alarmNotificationHandler failed to watch persist store changes ", err,
		)
	}
	return
}

func (h *alarmNotificationHandler) generateObjFromSubInfo(ctx context.Context,
	subInfo subscriptionInfo) (subInfoObj data.Object, err error) {
	err = h.jqTool.Evaluate(
		`{
		"alarmSubscriptionId": $alarmSubId,
		"consumerSubscriptionId": $consumerId,
		"callback": $callback,
		"filter": $filter
		}`,
		subInfoObj, &subInfoObj,
		jq.String("$alarmSubId", subInfo.subscriptionId),
	)

	return
}

// Interface called by db callback
/*
func (h *alarmNotificationHandler) processSubscritionInfoAdd(sub subscriptionInfo) {
	h.subscriptionMapMemoryLock.Lock()
	defer h.subscriptionMapMemoryLock.Unlock()

	h.subscriptionMap[sub.subscriptionId] = sub
	h.subscriptionIdSet[sub.subscriptionId] = struct{}{}

	var filters []string
	filters = strings.Split(sub.filter, ";")

	for _, filter := range filters {
		filter = strings.Trim(filter, "(")
		filter = strings.Trim(filter, ")")
		var sub_filters []string
		sub_filters = strings.Split(filter, ",")

		//assert len less than3
		if len(sub_filters) < 3 {
			panic("filter has less fields")
		}
		key := (sub_filters[0] + "+" + sub_filters[1])

		for i := 2; i < len(sub_filters); i++ {
			new_key := (key + "+" + sub_filters[i])
			_, ok := h.filterSubscriptionMap[new_key]
			if !ok {
				h.filterSubscriptionMap[new_key] = alarmSubIdSet{}
			}
			h.filterSubscriptionMap[new_key][sub.subscriptionId] = struct{}{}
		}
	}

}

func (h *alarmNotificationHandler) processSubscriptionInfoDelete() {
	h.subscritionNotificationMapMemoryLock.Lock()
	defer h.subscritionNotificationMapMemoryLock.Unlock()
}

func (h *alarmNotificationHandler) DbReadEntry(entry_key string) (value any, err error) {
	//read from DB for entry
	// now return nil and error
	return nil, ErrNotFound
}

func (h *alarmNotificationHandler) DbChangeNotify(entry_key string) {

	//read db entry
	_, err := h.DbReadEntry(entry_key)

	if err != ErrNotFound {
		//decode value
		//h.processSubscritionInfoAdd()
		return
	}
	h.processSubscriptionInfoDelete()
}

func (h *alarmNotificationHandler) RecoveryFromDb() {

	//for debug purpose now

	sub := subscriptionInfo{
		subscriptionId: "1e4ab660-e145-4bb0-8aa0-9b1aa7e12e40",
		consumerId:     "ef74487e-81ea-40ce-8b48-899b6310c3a7",
		filter:         "(eq,resourceID,my-host)",
		uris:           "https://my-smo.example.com/host-alarms",
		extensions:     make([]string, 0),
	}

	h.processSubscritionInfoAdd(sub)
	sub = subscriptionInfo{
		subscriptionId: "cdc24721-cddb-4bbb-a165-f6330ca79cf8",
		consumerId:     "69253c4b-8398-4602-855d-783865f5f25c",
		filter:         "(eq,extensions/country,US);(in,perceivedSeverity,CRITICAL,MAJOR)",
		uris:           "https://my-smo.example.com/country-alarms",
		extensions:     make([]string, 0),
	}


    //alarm example
	{
  "alarmEventRecordId": "a267bbd0-57aa-4ea1-b030-a300d420ef19",
  "resourceTypeID": "c1fe0c43-28e3-4b61-aac5-84bea67551ea",
  "resourceID": "my-host",
  "alarmDefinitionID": "4db97698-e612-430a-9520-c00e214c39e1",
  "probableCauseID": "4a02fdab-e135-4919-b60c-96af08bd088b",
  "alarmRaisedTime": "2012-04-23T18:25:43.511Z",
  "perceivedSeverity": "CRITICAL",
  "extensions": {
    "country": "US"
  }
}

	h.processSubscritionInfoAdd(sub)
} */

func (h *alarmNotificationHandler) recoveryFromPersistStore(ctx context.Context) (err error) {
	newMap, err := persiststorage.GetAll(h.persistStore, ctx)
	if err != nil {
		return
	}
	h.assignSubscriptionMap(newMap)
	return
}

func (h *alarmNotificationHandler) watchPersistStore(ctx context.Context) (err error) {
	err = persiststorage.ProcessChanges(h.persistStore, ctx, &h.subscriptionMap, h.subscriptionMapMemoryLock)

	if err != nil {
		panic("failed to launch watcher")
	}
	return
}

func (h *alarmNotificationHandler) addToNotificationMap(key string, value data.Object) {
	h.subscriptionMapMemoryLock.Lock()
	defer h.subscriptionMapMemoryLock.Unlock()
	(*h.subscriptionMap)[key] = value
}
func (h *alarmNotificationHandler) deleteToNotificationMap(key string) {
	h.subscriptionMapMemoryLock.Lock()
	defer h.subscriptionMapMemoryLock.Unlock()
	//test if the key in the map
	_, ok := (*h.subscriptionMap)[key]

	if !ok {
		return
	}

	delete(*h.subscriptionMap, key)
}

func (h *alarmNotificationHandler) assignSubscriptionMap(newMap map[string]data.Object) {
	h.subscriptionMapMemoryLock.Lock()
	defer h.subscriptionMapMemoryLock.Unlock()
	h.subscriptionMap = &newMap
}
