# Registry 
This is a high-level overview of the registry. 
The purpose of the registry is: 
- To read records from the provider and interpret them into the array of endpoints. 
- To translate an array of endpoints into that format that could be stored in the provider 

We use the `externaldns` implementation of endpoints. Metadata is stored in a labels map (`map[string]string`). Metadata is owner-specific, and we do not merge values from multiple owners (with the exception of `owner` labels and `soft_delete`). 


Each type of registry implements the `Registry` interface that provides access to the labels packer, ownerID and, the registry-specific filter of the endpoints. 

## TXT Registry
The TXT registry uses TXT records to store metadata. It is heavily inspired by the `external-dns` implementation of the registry. We create a TXT record per hostname+owner combination. 
The record name is `kuadrant-ownerID-recordType-hostname` and target is `heritage=external-dns,external-dns/key1=value1,external-dns/key2=value2`. 
Record will be created with one target for every key/value pair. 
Controller will be able to read records that will have multiple key/value pairs in one target.

TXT records are stored alongside endpoints in the provider. Note that the deletion of the DNSRecord/endpoint not always results in the deletion of the corresponding endpoint in the provider but will always result in the deletion of the corresponding TXT record. The same is true about creation. This is because multiple owners can define the same endpoint, but they will always define unique TXT records. 

The registry operates on the assumption that the `ownerID` is a string that does not contain `-` symbol (current `affixSeparator` ). The ID we use can be anything and not bound to be the `ownerID` exclusively. It is used only as a string to differentiate between TXT records for the same hostname from different owners. It does not carry any information. 