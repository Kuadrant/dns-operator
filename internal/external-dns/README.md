# ExternalDNS

To ease development, code reviews, and make it easier for developers to get up to speed with plan/registry/provider code, we will initially work off a copy of the relevant external-dns code in this repo. When we have a solution that works for us we will look into how we can submit that back to external-dns.

Code is copied, unmodified where possible, from the v0.14.0 version of external-dns. https://github.com/kubernetes-sigs/external-dns/tree/v0.14.0

If you are updating anything from the external-dns repo make sure you use this version, and keep all changes being made to external-dns code in their own commit and mark as "external-dns" in the comments. 