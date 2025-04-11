package registry

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/external-dns/endpoint"
)

func TestTXTLabelsPacker_LabelsPacked(t *testing.T) {
	tests := []struct {
		name    string
		labels  endpoint.Labels
		want    bool
		wantErr error
	}{
		{
			name: "unpacked labels",
			labels: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			want: false,
		},
		{
			name: "unpacked labels with key of owner length",
			labels: map[string]string{
				"12345678": "value1",
			},
			want: false,
		},
		{
			name: "packed labels",
			labels: map[string]string{
				"owner111": "key1=value1",
			},
			want: true,
		},
		{
			name: "packed empty owner",
			labels: map[string]string{
				"owner111": "",
			},
			want: true,
		},
		{
			name: "unknown owner",
			labels: map[string]string{
				"": "key1=value1",
			},
			want: true,
		},
		{
			name: "multiple owners",
			labels: map[string]string{
				"owner111": "key1=value1",
				"owner222": "key1=value1",
				"":         "key1=value1",
			},
			want: true,
		},
		{
			name: "multiple values",
			labels: map[string]string{
				"owner111": "key1=value1,key2=value2",
			},
			want: true,
		},
		{
			name: "mixed formats",
			labels: map[string]string{
				"key1":     "value1",
				"owner111": "key1=value1",
			},
			want:    false,
			wantErr: errors.New("unknown format"),
		},
		{
			name: "mixed values",
			labels: map[string]string{
				"owner111": "key1=value1",
				"owner222": "value1",
			},
			want:    false,
			wantErr: errors.New("unknown format"),
		},
		{
			name: "invalid packed format",
			labels: map[string]string{
				"owner111": "key1=value1,key2==",
			},
			want:    false,
			wantErr: errors.New("unknown format"),
		},
		{
			name:    "empty labels",
			labels:  map[string]string{},
			want:    false,
			wantErr: errors.New("no labels found"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := TXTLabelsPacker{}
			got, err := p.LabelsPacked(tt.labels)
			assert.Equal(t, tt.wantErr, err)
			assert.Equalf(t, tt.want, got, "LabelsUnPacked(%v)", tt.labels)
		})
	}
}

func TestTXTLabelsPacker_PackLabels(t *testing.T) {

	tests := []struct {
		name           string
		labelsPerOwner map[string]endpoint.Labels
		want           endpoint.Labels
	}{
		{
			name: "pack labels",
			labelsPerOwner: map[string]endpoint.Labels{
				"owner111": {
					"key1": "value1",
					"key2": "value2",
				},
				"owner222": {
					"key1": "value1",
					"key2": "value2",
				},
			},
			want: endpoint.Labels{
				"owner111": "key1=value1,key2=value2",
				"owner222": "key1=value1,key2=value2",
			},
		},
		{
			name: "pack empty labels",
			labelsPerOwner: map[string]endpoint.Labels{
				"owner111": {},
			},
			want: endpoint.Labels{
				"owner111": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := TXTLabelsPacker{}
			got := p.PackLabels(tt.labelsPerOwner)
			for wantKey, wantValue := range tt.want {
				_, ok := got[wantKey]
				assert.True(t, ok, "key %s not found in labels map", wantKey)
				wantValueSplit := strings.Split(wantValue, labelsSeparator)
				for _, singleWantValueSplit := range wantValueSplit {
					assert.Contains(t, got[wantKey], singleWantValueSplit)
				}
			}
		})
	}
}

func TestTXTLabelsPacker_UnpackLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels endpoint.Labels
		want   map[string]endpoint.Labels
	}{
		{
			name: "unpack labels",
			labels: endpoint.Labels{
				"owner111": "key1=value1,key2=value2",
				"owner222": "key1=value1,key2=value2",
			},
			want: map[string]endpoint.Labels{
				"owner111": {
					"key1": "value1",
					"key2": "value2",
				},
				"owner222": {
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		{
			name: "unpack empty labels",
			labels: endpoint.Labels{
				"owner111": "",
			},
			want: map[string]endpoint.Labels{
				"owner111": {},
			},
		},
		{
			name: "unpack unpacked labels",
			labels: endpoint.Labels{
				"key1": "value1",
				"key2": "value2",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := TXTLabelsPacker{}
			assert.Equalf(t, tt.want, p.UnpackLabels(tt.labels), "UnpackLabels(%v)", tt.labels)
		})
	}
}
