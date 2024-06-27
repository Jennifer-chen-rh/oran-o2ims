package service

const (
	//default namespace should be changed to official namespace when it is available
	DefaultNamespace                   = "orantest"
	DefaultAlarmConfigmapName          = "oran-alarms-sub"
	DefaultInfraInventoryConfigmapName = "oran-infra-inventory-sub"
	FieldOwner                         = "oran-o2ims"
)

const (
	SubscriptionIdAlarm                   = "alarmSubscriptionId"
	SubscriptionIdInfrastructureInventory = "subscriptionId"
)

func isSubscriptionAlarm(idString string) bool {
	return idString == SubscriptionIdAlarm
}

func isSubscriptionInfrastructureInventory(idString string) bool {
	return idString == SubscriptionIdInfrastructureInventory
}
