package v1alpha1

type ConditionType string
type ConditionReason string

const ConditionTypeReady ConditionType = "Ready"
const ConditionReasonProviderSuccess ConditionReason = "ProviderSuccess"
const ConditionReasonProviderError ConditionReason = "ProviderError"
const ConditionReasonAwaitingValidation ConditionReason = "AwaitingValidation"
const ConditionReasonProviderEndpointsRemoved ConditionReason = "ProviderEndpointsRemoved"
const ConditionReasonProviderEndpointsDeletion ConditionReason = "ProviderEndpointsDeletion"
const ConditionReasonValidationError ConditionReason = "ValidationError"

const ConditionTypeHealthy ConditionType = "Healthy"
const ConditionReasonHealthy ConditionReason = "AllChecksPassed"
const ConditionReasonPartiallyHealthy ConditionReason = "SomeChecksPassed"
const ConditionReasonUnhealthy ConditionReason = "HealthChecksFailed"

const ConditionTypeReadyForDelegation ConditionType = "ReadyForDelegation"
const ConditionReasonFinalizersSet ConditionReason = "FinalizersSet"
