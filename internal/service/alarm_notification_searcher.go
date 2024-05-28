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
	"errors"
	"strings"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
)

type subscriptionFilter struct {
	operation string
	resource  string
	values    string
}

type subscriptionInfo struct {
	subscriptionId string
	filters        []subscriptionFilter
	entities       map[string]struct{}
	//extensions     []string
}

// This file contains oran alarm notification serer search for matched subscriptions
// at 1st step apply linear search
type alarmSubscriptionSearcher struct {
	subscriptionSearcherMap *map[string]subscriptionInfo
}

func newAlarmSubscriptionSearcher() *alarmSubscriptionSearcher {
	return &alarmSubscriptionSearcher{}
}

func (b *alarmSubscriptionSearcher) init() {
	b.subscriptionSearcherMap = &map[string]subscriptionInfo{}
}

func getSubFilters(filterStr string) (result []subscriptionFilter, entities map[string]struct{}, err error) {
	var filterStrings []string
	result = []subscriptionFilter{}
	entities = map[string]struct{}{}

	//no filter found, return empty array and behavior as "*"
	if filterStr == "" {
		return
	}

	filterStrings = strings.Split(filterStr, ";")
	for _, filter := range filterStrings {
		filter_cp := filter
		filter = strings.Trim(filter, "(")
		filter = strings.Trim(filter, ")")
		var sub_filters []string
		sub_filters = strings.Split(filter, ",")

		if len(sub_filters) < 3 {
			e := ("Filter " + filter_cp + "is mal-formatted")
			err = errors.New(e)
			continue
		}
		result = append(result, subscriptionFilter{
			operation: sub_filters[0],
			resource:  sub_filters[1],
			values:    sub_filters[2],
		})
		entities[sub_filters[1]] = struct{}{}
	}

	return
}

func ProcessSubscriptionMapForSearcher(subscriptionMap *map[string]data.Object,
	jqTool *jq.Tool) (result map[string]subscriptionInfo) {
	result = map[string]subscriptionInfo{}

	for key, value := range *subscriptionMap {
		//get filter from data object
		var filter string
		jqTool.Evaluate(`.filter`, value, &filter)
		filters, entities, _ := getSubFilters(filter)

		result[key] = subscriptionInfo{
			subscriptionId: key,
			filters:        filters,
			entities:       entities,
		}
	}

	return
}

func (h *alarmNotificationHandler) getSubscriptionIdsFromAlarm(alarm data.Object) (result alarmSubIdSet) {

	result = alarmSubIdSet{}

	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.Unlock()

	for key, value := range *h.subscriptionSearcher.subscriptionSearcherMap {

		//no filter (*)
		if value.filters == nil {
			result[key] = struct{}{}
			continue
		}

	}

	return
}
