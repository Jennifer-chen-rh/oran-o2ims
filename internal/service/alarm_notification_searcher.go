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
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

type subscriptionInfo struct {
	subscriptionId         string
	filters                search.Selector
	uris                   string
	consumerSubscriptionId string
	//entities       map[string]struct{}
	//extensions     []string
}

// This file contains oran alarm notification serer search for matched subscriptions
// at 1st step apply linear search
type alarmSubscriptionSearcher struct {
	logger *slog.Logger
	//maps with prebuilt selector
	subscriptionInfoMap *map[string]subscriptionInfo
	pathIndexMap        *map[string]alarmSubIdSet
	noFilterSubsSet     *alarmSubIdSet

	//Parser used for the subscription filters
	selectorParser *search.SelectorParser
}

func (b *alarmSubscriptionSearcher) SetLogger(
	value *slog.Logger) *alarmSubscriptionSearcher {
	b.logger = value
	return b
}

func newAlarmSubscriptionSearcher() *alarmSubscriptionSearcher {
	return &alarmSubscriptionSearcher{}
}

func (b *alarmSubscriptionSearcher) build() {
	b.subscriptionInfoMap = &map[string]subscriptionInfo{}
	b.pathIndexMap = &map[string]alarmSubIdSet{}
	b.noFilterSubsSet = &alarmSubIdSet{}

	// Create the filter expression parser:
	selectorParser, err := search.NewSelectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		b.logger.Error("failed to create filter expression parser: %w", err)
		return
	}
	b.selectorParser = selectorParser
}

func (b *alarmSubscriptionSearcher) getSubFilters(filterStr string, subId string) (err error) {

	//no filter found, return empty array and behavior as "*"
	if filterStr == "" {
		(*b.noFilterSubsSet)[subId] = struct{}{}
		return
	}

	result, err := b.selectorParser.Parse(filterStr)

	if err != nil {
		return
	}

	//for now use path 0 only
	//to be fixed with full path for quicker search
	for _, element := range result.Terms {
		_, ok := (*b.pathIndexMap)[element.Path[0]]

		if !ok {
			(*b.pathIndexMap)[element.Path[0]] = alarmSubIdSet{}
		}
		(*b.pathIndexMap)[element.Path[0]][subId] = struct{}{}
	}

	return
}

func (b *alarmSubscriptionSearcher) pocessSubscriptionMapForSearcher(subscriptionMap *map[string]data.Object,
	jqTool *jq.Tool) (err error) {

	for key, value := range *subscriptionMap {
		//get filter from data object
		var filter string
		jqTool.Evaluate(`.filter`, value, &filter)
		err = b.getSubFilters(filter, key)
		if err != nil {
			b.logger.Debug(
				"pocessSubscriptionMapForSearcher ",
				"subscription: ", key,
				" error: ", err.Error(),
			)
		}
	}
	return
}

func (h *alarmNotificationHandler) getSubscriptionIdsFromAlarm(ctx context.Context, alarm data.Object) (result alarmSubIdSet) {

	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.Unlock()
	result = *h.subscriptionSearcher.noFilterSubsSet

	for path, subSet := range *h.subscriptionSearcher.pathIndexMap {

		var alarmPath string
		h.jqTool.Evaluate(path, alarm, alarmPath)

		if alarmPath != "" {
			for subId, _ := range subSet {
				subInfo := (*h.subscriptionSearcher.subscriptionInfoMap)[subId]
				match, err := h.selectorEvaluator.Evaluate(ctx, &subInfo.filters, alarm)
				if err != nil {
					h.logger.Debug(
						"pocessSubscriptionMapForSearcher ",
						"subscription: ", subInfo.subscriptionId,
						" error: ", err.Error(),
					)
					continue
				}
				if match {
					result[subId] = struct{}{}
				}
			}
		}

	}

	return
}

func (h *alarmNotificationHandler) getSubscriptionInfo(ctx context.Context, subId string) (result subscriptionInfo, ok bool) {
	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.Unlock()
	result, ok = (*h.subscriptionSearcher.subscriptionInfoMap)[subId]
	return
}
