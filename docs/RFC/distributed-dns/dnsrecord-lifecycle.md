# List of issues
* [Re-queue validation intermittently GH-36](https://github.com/Kuadrant/dns-operator/issues/36)
* [Re-queue DNS Record whenever a write to the Cloud Provider occurs GH-35](https://github.com/Kuadrant/dns-operator/issues/35)
* [Schedule removal of finalizer from DNS Records GH-38](https://github.com/Kuadrant/dns-operator/issues/38)
* [Record write attempts in status for current generation GH-34](https://github.com/Kuadrant/dns-operator/issues/34)

# The idea
We now will constantly reconcile DNS records. The reasoning is that other controllers may override/change records in the DNS provider so there is a need to requeue the DNS Record from time to time even when no local changes are introduced.


# Details
There are a few fields on the DNS Record status:
* ValidFor indicates the current requeue interval for the record, used for exponential backoff
* WriteCounter represents a number of consecutive write attempts on the same generation of the record. It is being reset to 0 when the generation changes or there are no changes to write.


There is an option to override the `DefaultRequeueTime` with the `requeue-time` flag.


The `DefaultRequeueTime` is the maximum duration between reconciliations to ensure that the record is still up-to-date. The requeue interval grows exponentially from `MinRequeueTime` up to `DefaultRequeueTime`.


## DNS Record normal lifecycle
Once we enqueue the DNS record, controller will compile a list of changes to the DNS provider and will apply it. After this, the record is enqueued with the `validationRequeueTime` and the `Ready` condition will be marked as `false` with a message `Awaiting Validation`. When the record is received again and the controller ensures there are no changes needed (the ones applied are present in the DNS Provider) it sets the `Ready` condition to `true` and enqueues it with the `defaultRequeueTime`.


Upon deletion, the process will be similar. The controller will determine the changes needed to the DNS provider and will apply them. The record will be requeued with the `validationRequeueTime`. Once we receive it back and ensure that there are no changes needed for the DNS provider we remove the finalizer from the record.


The `validationRequeueTime` duration is randomized +/- 50%.


## When things go south
Every reconciliation runs the full reconciliation flow. The requeue interval uses exponential backoff to limit DNS provider API calls.


When we encounter an error during the reconciliation we will not requeue the record and will put in an appropriate error message in the log and on the record. In order for it to reconcile again there must be a change to the DNS Record CR.

In case the controller fails to retain changes in the DNS Provider: write are successful, but the validation fails again and the `WriteCounter` reaches the `WriteCounterLimit` we give up on the reconciliation. The appropriate message will be put under the `Ready - false` condition as well as in the logs of the controller. The reconciliation will resume once the generation of the DNS Record is changed.

## Metrics
There is a metric emitted from the controller: `dns_provider_write_counter`. It reflects the `WriteCounter` field in the status of the record.

