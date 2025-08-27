package metrics

import (
	"strings"
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func Test_EndpointMetricSha(t *testing.T) {
	testCases := []struct {
		Name     string
		Expected bool
		RecordA  *v1alpha1.DNSRecord
		RecordB  *v1alpha1.DNSRecord
	}{
		{
			Name:     "Two DNS equal hash with same layout",
			Expected: true,
			RecordA: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordA",
					Namespace: "RecordA",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{

							DNSName: "simple.kuadrant.local",

							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
					},
				},
			},
			RecordB: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordB",
					Namespace: "RecordB",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:   "simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
					},
				},
			},
		},
		{
			Name:     "Two DNS equal hash with different layout",
			Expected: true,
			RecordA: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordA",
					Namespace: "RecordA",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{

							DNSName: "simple.kuadrant.local",

							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
					},
				},
			},
			RecordB: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordB",
					Namespace: "RecordB",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"odds":  "sods",
								"owner": "test",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.2",
								"172.18.200.1",
							},
						},
						{
							DNSName:   "simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
					},
				},
			},
		},
		{
			Name:     "Two DNS none hash with different layout",
			Expected: false,
			RecordA: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordA",
					Namespace: "RecordA",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{

							DNSName: "simple.kuadrant.local",

							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"owner": "test",
								"odds":  "sods",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.1",
								"172.18.200.2",
							},
						},
					},
				},
			},
			RecordB: &v1alpha1.DNSRecord{
				ObjectMeta: v1.ObjectMeta{
					Name:      "RecordB",
					Namespace: "RecordB",
				},
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "simple.kuadrnat.local",
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:   "demo.simple.kuadrant.local",
							RecordTTL: 60,
							Labels: map[string]string{
								"odds":  "sods",
								"owner": "test",
							},
							RecordType: "A",
							Targets: externaldns.Targets{
								"172.18.200.2",
								"172.18.200.1",
							},
						},
					},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			recordA, err := NewAuthoritativeRecordSpecInfoMetric(test.RecordA)
			if err != nil {
				t.Fatalf("error parsing record, error: %v", err.Error())
			}

			recordB, err := NewAuthoritativeRecordSpecInfoMetric(test.RecordB)
			if err != nil {
				t.Fatalf("error parsing record, error: %v", err.Error())
			}

			result := (recordA.Sha == recordB.Sha)
			if result != test.Expected {
				t.Fatalf("Comparing recordA.Sha (%s) to recordB.Sha (%s) was not the expected result of: %v", recordA.Sha, recordB.Sha, test.Expected)
			}
		})
	}
}

func Test_NewEndpointCounterMetricNullPointer(t *testing.T) {
	testcase := []struct {
		Name      string
		DNSRecord *v1alpha1.DNSRecord
		ExpectErr bool
		ErrMsg    string
	}{
		{
			Name:      "full null pointer",
			DNSRecord: nil,
			ExpectErr: true,
			ErrMsg:    "dnsRecord nil pointer",
		},
		{
			Name:      "minium reference",
			DNSRecord: &v1alpha1.DNSRecord{},
			ExpectErr: false,
		},
	}

	for _, test := range testcase {
		t.Run(test.Name, func(t *testing.T) {
			_, err := NewAuthoritativeRecordSpecInfoMetric(test.DNSRecord)

			if test.ExpectErr && err == nil {
				t.Fatalf("Was expecting an error, but none was found")
			}

			if err != nil && !strings.Contains(err.Error(), test.ErrMsg) {
				t.Fatalf("Error message not equal expected; error: %s, expected: %s", err.Error(), test.ErrMsg)
			}

		})
	}

}
