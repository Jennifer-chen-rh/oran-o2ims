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

type subscriptionInfo struct {
	subscriptionId string
	filter         string
	consumerId     string
	uris           string
	extensions     []string
}

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
	subscriptionIdSet         alarmSubIdSet
	filterSubscriptionMap     map[string]alarmSubIdSet
	persistStore              *persiststorage.KubeConfigMapStore
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

	// create persist storeage option
	persistStore := persiststorage.NewKubeConfigMapStore().
		SetNameSpace(TestNamespace).
		SetName(TestConfigmapName).
		SetFieldOwnder(FieldOwner).
		SetJsonAPI(&jsonAPI).
		SetClient(b.kubeClient)

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
		subscriptionIdSet:         alarmSubIdSet{},
		filterSubscriptionMap:     map[string]alarmSubIdSet{},
		persistStore:              persistStore,
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
		jq.String("$consumerId", subInfo.consumerId),
		jq.String("$callback", subInfo.uris),
		jq.String("$filter", subInfo.filter),
	)

	return
}

// Add is the implementation of the object handler ADD interface.
// receive obsability alarm post and trigger the alarms
func (h *alarmNotificationHandler) Add(ctx context.Context,
	request *AddRequest) (response *AddResponse, err error) {

	h.logger.Debug(
		"alarmNotificationHandler Add",
	)

	//process alarm notification from observility
	//for now process selected alarm fields
	//in future needs loop through all alarm fields for filters
	//resourceID
	id_set := make(alarmSubIdSet)

	//get event ID for log purpose now
	var eventRecordId string
	h.jqTool.Evaluate(`.alarmEventRecordId`, request.Object, &eventRecordId)

	var resourceID string
	err = h.jqTool.Evaluate(
		`.resourceID`, request.Object, &resourceID)

	in_id_set := make(alarmSubIdSet)
	filter_checked_set := make(alarmSubIdSet)
	not_in_id_set := make(alarmSubIdSet)
	if err == nil {
		//check resource ID related filter
		// include
		for _, in_filter := range filter_include_strings {
			new_key := (in_filter + "+" + "resourceID" + "+" + resourceID)
			sub_ids, ok := h.filterSubscriptionMap[new_key]
			if ok {
				for k, v := range sub_ids {
					_, ok = filter_checked_set[k]
					/*if ok {
						continue
					}*/
					in_id_set[k] = v
					filter_checked_set[k] = struct{}{}
				}

			}
		}

		// exclude
		for _, ex_filter := range filter_exclude_strings {
			new_key := (ex_filter + "+" + "resourceID" + "+" + resourceID)
			sub_ids, ok := h.filterSubscriptionMap[new_key]
			if ok {
				//meet not in condition
				for k, _ := range sub_ids {
					not_in_id_set[k] = struct{}{}
				}
			} else {

			}
		}
	}

	//perceivedSeverity
	var perceivedSeverity string
	err = h.jqTool.Evaluate(
		`.perceivedSeverity`, request.Object, &perceivedSeverity)
	if err == nil {
		//check perceivedSeverity related filter
		// include
		for _, in_filter := range filter_include_strings {
			new_key := (in_filter + "+" + "perceivedSeverity" + "+" + perceivedSeverity)
			sub_ids, ok := h.filterSubscriptionMap[new_key]
			if ok {
				for k, v := range sub_ids {
					_, ok = filter_checked_set[k]
					if ok {
						continue
					}
					in_id_set[k] = v
					filter_checked_set[k] = struct{}{}
				}
			}
		}

		// exclude
		for _, ex_filter := range filter_exclude_strings {
			new_key := (ex_filter + "+" + "perceivedSeverity" + "+" + perceivedSeverity)
			sub_ids, ok := h.filterSubscriptionMap[new_key]
			if ok {
				//meet not in condition
				for k, _ := range sub_ids {
					not_in_id_set[k] = struct{}{}
				}
			}
		}

	}

	star_ids, ok := h.filterSubscriptionMap["*"]
	if ok {
		for k, v := range star_ids {
			if ok {
				id_set[k] = v
			}
		}
	}

	for k, v := range in_id_set {
		id_set[k] = v
	}

	for k, _ := range not_in_id_set {
		delete(id_set, k)
	}

	//now look id_set and send http packets to URIs
	/*
		for key, _ := range id_set {
			subInfo := h.subscriptionMap[key]

			//var subInfoObj data.Object
			_, err := h.generateObjFromSubInfo(ctx, subInfo)

			if err != nil {
				h.logger.Debug("alarmNotificationHandler build subinfo error %s", err.Error())
			}

			h.logger.Debug("alarmNotificationHandler build post for subscription %s, event %s",
				subInfo.subscriptionId, eventRecordId)
			//Following send post request with subscribeInfo +
				http.Post(subInfo.uris, "application/json", subInfoObj+eventObj)

				data.Object  ==> string in json format// json.Mashal

		}*/

	// Return the result:
	eventObj := request.Object
	response = &AddResponse{
		Object: eventObj,
	}
	return
}

/* per alarm type and retrieve subscription IDs set */
func (h *alarmNotificationHandler) getSubscriptionIds(alarmType string) (result alarmSubIdSet) {

	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.Unlock()
	result, ok := h.filterSubscriptionMap[alarmType]

	if !ok {
		return alarmSubIdSet{}
	}

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
