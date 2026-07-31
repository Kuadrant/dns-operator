# List of issues
* [Re-queue validation intermittently GH-36](https://github.com/Kuadrant/dns-operator/issues/36)
* [Re-queue DNS Record whenever a write to the Cloud Provider occurs GH-35](https://github.com/Kuadrant/dns-operator/issues/35)
* [Schedule removal of finalizer from DNS Records GH-38](https://github.com/Kuadrant/dns-operator/issues/38)
* [Record write attempts in status for current generation GH-34](https://github.com/Kuadrant/dns-operator/issues/34)

# The idea
We now will constantly reconcile DNS records. The reasoning is that other controllers may override/change records in the DNS provider so there is a need to requeue the DNS Record from time to time even when no local changes are introduced.


# Details
There is a status field on the DNS Record:
* WriteCounter represents a number of consecutive write attempts on the same generation of the record. It is being reset to 0 when the generation changes or there are no changes to write.

Reconciliation backoff is handled by the controller-runtime SDK's rate limiter, configured with `--min-requeue-time` (base delay, default 5s) and `--max-requeue-time` (max delay, default 15m). The rate limiter provides per-item exponential backoff: each requeue doubles the delay until the max is reached. The backoff resets when the record's generation changes.


## DNS Record normal lifecycle
Once we enqueue the DNS record, controller will compile a list of changes to the DNS provider and will apply it. After this, the record is enqueued with the `minRequeueTime` and the `Ready` condition will be marked as `false` with a message `Awaiting Validation`. When the record is received again and the controller ensures there are no changes needed (the ones applied are present in the DNS Provider) it sets the `Ready` condition to `true` and the SDK rate limiter begins ramping up the requeue interval exponentially toward `maxRequeueTime`.


Upon deletion, the process will be similar. The controller will determine the changes needed to the DNS provider and will apply them. The record will be requeued with the `minRequeueTime`. Once we receive it back and ensure that there are no changes needed for the DNS provider we remove the finalizer from the record.


## When things go south
When we encounter an error during the reconciliation we will requeue the record with rate-limited exponential backoff and put an appropriate error message in the log and on the record.


In case the controller fails to retain changes in the DNS Provider: writes are successful, but the validation fails again and the `WriteCounter` reaches the `WriteCounterLimit` we give up on the reconciliation. The appropriate message will be put under the `Ready - false` condition as well as in the logs of the controller. The reconciliation will resume once the generation of the DNS Record is changed.

## Metrics
There is a metric emitted from the controller: `dns_provider_write_counter`. It reflects the `WriteCounter` field in the status of the record.
