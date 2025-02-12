f.example.com.   IN SOA ns1.f.example.com. root.f.example.com. 2015082541 7200 3600 1209600 3600
f.example.com.   IN NS  ns1.f.example.com.
rec1.api.f.example.com.   IN A   1.1.1.1
rec1.api.f.example.com.   IN A   2.2.2.2
rec2.api.f.example.com.   IN A   3.3.3.3
rec2.api.f.example.com.   IN A   4.4.4.4
lb.api.f.example.com.   IN CNAME  rec1.api.f.example.com.
api.f.example.com.   IN CNAME  lb.api.f.example.com.
*.api.f.example.com. IN A   9.9.9.9
