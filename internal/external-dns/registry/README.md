# Registry 
This is a high-level overview of the registry. 
The purpose of the registry is: 
- To read records from the provider and interpret them into the array of endpoints. 
- To translate an array of endpoints into that format that could be stored in the provider 

We use the `externaldns` implementation of endpoints. Metadata is stored in a labels map (`map[string]string`). Metadata is owner-specific, and we do not merge values from multiple owners. 

The structure of labels: 
- "ownerID-1" : "key1=value1,key2=value2"
- "ownerID-2" : "key1=value1,key2=value2"

Each registry contains "labels packer" that provides conversion of labels to and from the following structure: 

- "ownerID-1" :
  - "key1" : "value1"
  - "key2" : "value2"
- "ownerID-2"
  - "key1" : "value1"
  - "key2" : "value2"

Each type of registry implements the `Registry` interface that provides access to the labels packer, ownerID and, the registry-specific filter of the endpoints. 

## TXT Registry
The TXT registry uses TXT records to store metadata. We create a TXT record per hostname+owner combination. 
The record name is `kuadrant-ownerID-recordType-hostname` and target is `heritage=external-dns,external-dns/key1=value1`. 
Record will be created with one target per key/value pair. 
Controller will be able to read records that will have multiple key/value pairs in one target. Also, if there are no extra labels to be stored the target will be `""` (not and empty string!)

TXT records are stored alongside endpoints in the provider. Note that the deletion of the DNSRecord/endpoint not always results in the deletion of the corresponding endpoint in the provider but will always result in the deletion of the corresponding TXT record. The same is true about creation. This is because multiple owners can define the same endpoint, but they will always define unique TXT records. 