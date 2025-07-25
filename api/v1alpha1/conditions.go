package v1alpha1

type ConditionType string
type ConditionReason string

const ConditionTypeReady ConditionType = "Ready"
const ConditionReasonProviderSuccess ConditionReason = "ProviderSuccess"
const ConditionReasonAwaitingValidation ConditionReason = "AwaitingValidation"
const ConditionReasonProviderEndpointsRemoved ConditionReason = "ProviderEndpointsRemoved"
const ConditionReasonProviderEndpointsDeletion ConditionReason = "ProviderEndpointsDeletion"

const ConditionTypeHealthy ConditionType = "Healthy"
const ConditionReasonHealthy ConditionReason = "AllChecksPassed"
const ConditionReasonPartiallyHealthy ConditionReason = "SomeChecksPassed"
const ConditionReasonUnhealthy ConditionReason = "HealthChecksFailed"
