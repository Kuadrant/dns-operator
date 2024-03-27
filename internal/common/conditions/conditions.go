package conditions

type ConditionType string
type ConditionReason string

// TODO move to the API
const ConditionTypeReady ConditionType = "Ready"

/*
when queued for - time of the next full reconcile / repeat reconcile
fail counter
time when queued for the reconcile
expiration time for validity of data

11:00 i've queued
11:15 when to reconcile


scenarios:
full reconcile
hit when now > queued + valid
generation changes

short one
when now < queued + valid



fail counter
if gen has not changed and i need to write it is a fail





reset counter when gen changes
when validation loop succeeds





*/
