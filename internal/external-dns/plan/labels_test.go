package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnsureLabel(t *testing.T) {
	type args struct {
		labels string
		label  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "adds label if missing",
			args: args{
				labels: "label1",
				label:  "label2",
			},
			want: "label1" + LabelDelimiter + "label2",
		},
		{
			name: "does nothing if label is present",
			args: args{
				labels: "label1" + LabelDelimiter + "label2",
				label:  "label2",
			},
			want: "label1" + LabelDelimiter + "label2",
		},
		{
			name: "adds label to empty list",
			args: args{
				labels: "",
				label:  "label2",
			},
			want: "label2",
		},
		{
			name: "ignores an empty label",
			args: args{
				labels: "label1",
				label:  "",
			},
			want: "label1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, EnsureLabel(tt.args.labels, tt.args.label), "EnsureLabel(%v, %v)", tt.args.labels, tt.args.label)
		})
	}
}

func TestRemoveLabel(t *testing.T) {
	type args struct {
		labels string
		label  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "removes label if present",
			args: args{
				labels: "label1" + LabelDelimiter + "label2",
				label:  "label2",
			},
			want: "label1",
		},
		{
			name: "does nothing if label is missing",
			args: args{
				labels: "label1",
				label:  "label2",
			},
			want: "label1",
		},
		{
			name: "removes label from empty list",
			args: args{
				labels: "",
				label:  "label2",
			},
			want: "",
		},
		{
			name: "ignores an empty label",
			args: args{
				labels: "label1",
				label:  "",
			},
			want: "label1",
		},
		{
			name: "removes the only label",
			args: args{
				labels: "label1",
				label:  "label1",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, RemoveLabel(tt.args.labels, tt.args.label), "RemoveLabel(%v, %v)", tt.args.labels, tt.args.label)
		})
	}
}

func TestSplitLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels string
		want   []string
	}{
		{
			name:   "splits labels",
			labels: "label1" + LabelDelimiter + "label2",
			want:   []string{"label1", "label2"},
		},
		{
			name:   "splits one label",
			labels: "label1",
			want:   []string{"label1"},
		},
		{
			name:   "splits empty labels",
			labels: "",
			want:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, SplitLabels(tt.labels), "SplitLabels(%v)", tt.labels)
		})
	}
}
