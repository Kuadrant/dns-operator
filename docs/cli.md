# Overview 
The `kuadrant-dns` (`kubect-kuadrant_dns` is the binary name) is a CLI that is shipped alongside the DNS-operator. 

It is intended to be used for advanced configuration (such as automated configuration of cluster secrets for CoreDNS), gathering of the debug information (i.e. get all records from the managed zone), and to manually adjust the managed zone (i.e. delete the owner). See `kubectl kuadrant-dns help` for a list of available functions

If located in `PATH`, it will act as a kubectl plugin and will embed itself into `kuadrantctl`, but can be used as a standalone. 


# How to get

As a note - there is a `make cp-cli` target that will move the binary from `dns-operator/bin` into `~/.local/bin`. 

## From source 
This requires `GO` configured on your machine
1. Clone this repository 
2. Run `make build-cli`
3. Use the binary from the `dns-operator/bin` 


## From repository 
You can download a binary for your architecture from the [release](https://github.com/Kuadrant/dns-operator/releases) page of the DNS-operator. A new version of the cli is built on each release after v0.14.0

# Commands
For most cases running a command with `--help` should answer your questions. Here you will find less of a technical details
but of a more reasoning and intent behind commands. 

## DNS Groups 
The DNS Group commands will allow the manipulation of the `kuadrant-active-groups.<domain>` TXT record.
This record is responsible for informing controllers which DNS Groups are currently active.
The DNS Group commands are intended to be run with a secret that has list permissions across all zones 
and write permissions in the relevant zones.

### Global Flags

--verbose / -v Set the log level for the cli. All logs are sent to standard error. 

- level 0: default level, error logs will be exposed
- level 1: error, and info logs will be exposed
- level 2: error, info, and debug logs will be exposed

### add-active-group 
Will fetch a list of zones that are an exact match of a domain (the `--domain` flag).
You can specify `*.<domain>` to match all zones that end with `<domain>`.
If more than one zone is found it will prompt asking which of the zones to select, 
unless `-y` is provided in which case it will apply the change to all relevant zones..
When adding a group to the set of active groups in the selected zone the command will first ensure the group is not already present.
If the group is not present, it will either update the existing `kuadrant-active-groups.<domain>` TXT record, or create it. 

### get-active-group
Will fetch a list of active groups from the `kuadrant-active-groups.<domain>` TXT record and display them. 
Active groups will be listed under the corresponding zone.

### remove-active-group
Will fetch a list of zones that are an exact match of a domain (the `--domain` flag).
You can specify `*.<domain>` to match all zones that end with `<domain>`.
For each zone that has a group in the `kuadrant-active-groups.<domain>` TXT record it will display all endpoints that 
are associated with the group. Then it will prompt with a confirmation of a deletion unless `-y`/`--assumeyes` flag was provided; 
in that case it will proceed.