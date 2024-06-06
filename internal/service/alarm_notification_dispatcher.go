package service

import (
	"bytes"
	"context"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

// Add is the implementation of the object handler ADD interface.
// receive obsability alarm post and trigger the alarms

func (h *alarmNotificationHandler) Add(ctx context.Context,
	request *AddRequest) (response *AddResponse, err error) {

	h.logger.Debug(
		"alarmNotificationHandler Add",
	)

	var eventRecordId string
	h.jqTool.Evaluate(`.alarmEventRecordId`, request.Object, &eventRecordId)

	subIdSet := h.getSubscriptionIdsFromAlarm(ctx, request.Object)

	//build the packet
	var packet data.Object

	if err != nil {
		h.logger.Debug("alarmNotificationHandler failed to marshal %s", err.Error())
		return
	}

	//now look id_set and send http packets to URIs
	for key, _ := range subIdSet {
		subInfo, ok := h.getSubscriptionInfo(ctx, key)

		if !ok {
			h.logger.Debug("alarmNotificationHandler failed to get subinfo key %s", key)
			continue
		}

		//NOTE:
		// alarmNotificationType needs to be added
		// objectRef needs to be added
		packet["consumerSubscriptionId"] = subInfo.consumerSubscriptionId
		packet["alarmEventRecord"] = request.Object

		go func(pkt data.Object) {
			content, err := h.jsonAPI.Marshal(packet)
			if err != nil {
				h.logger.Debug("alarmNotificationHandler failed to get content of new packet %s", err.Error())
			}
			resp, err := h.httpClient.Post(subInfo.uris, "application/json", bytes.NewBuffer(content))
			if err != nil {
				h.logger.Debug("alarmNotificationHandler failed to post packet %s", err.Error())
				return
			}

			defer resp.Body.Close()
		}(packet)

	}

	eventObj := request.Object
	response = &AddResponse{
		Object: eventObj,
	}
	return
}

/*func (h *alarmNotificationHandler) Add(ctx context.Context,
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
/*in_id_set[k] = v
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

/*// Return the result:
	eventObj := request.Object
	response = &AddResponse{
		Object: eventObj,
	}
	return
}

/* per alarm type and retrieve subscription IDs set */
/*func (h *alarmNotificationHandler) getSubscriptionIds(alarmType string) (result alarmSubIdSet) {

	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.Unlock()
	result, ok := h.filterSubscriptionMap[alarmType]

	if !ok {
		return alarmSubIdSet{}
	}

	return
}
*/
