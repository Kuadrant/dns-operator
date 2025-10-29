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