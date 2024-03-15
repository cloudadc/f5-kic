package main

import (
	"reflect"
	"sync"
	"testing"

	"github.com/f5devcentral/f5-bigip-rest-go/utils"
)

func Test_struct2string(t *testing.T) {
	type args struct {
		r          any
		skipFields []string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
		{
			name: "DeployRequest",
			args: args{
				r: DeployRequest{
					Operation: "oper",
					Kind:      "ltm/virtual",
					NewaRef:   nil,
					Name:      "queue cm",
				},
				skipFields: []string{},
			},
			want: "Timestamp: 0001-01-01 00:00:00 +0000 UTC, Operation: oper, Partition: , Subfolder: , Name: queue cm, Kind: ltm/virtual, Retries: 0, Context: <nil>, NewaRef: <nil>, OrigRef: <nil>, ObjRefs: [], Relevants: []",
		},
		{
			name: "DeployRequest skip nil",
			args: args{
				r: DeployRequest{
					Operation: "oper",
					Kind:      "ltm/virtual",
					NewaRef:   nil,
					Name:      "queue cm",
				},
				skipFields: []string{"Timestamp", "Context"},
			},
			want: "Operation: oper, Partition: , Subfolder: , Name: queue cm, Kind: ltm/virtual, Retries: 0, NewaRef: <nil>, OrigRef: <nil>, ObjRefs: [], Relevants: []",
		},
		{
			name: "DeployRequest skip normal",
			args: args{
				r: DeployRequest{
					Operation: "oper",
					Kind:      "ltm/virtual",
					NewaRef:   nil,
					Name:      "queue cm",
				},
				skipFields: []string{"Timestamp", "Context", "Kind"},
			},
			want: "Operation: oper, Partition: , Subfolder: , Name: queue cm, Retries: 0, NewaRef: <nil>, OrigRef: <nil>, ObjRefs: [], Relevants: []",
		},

		{
			name: "DeployRequest skip normal",
			args: args{
				r: DeployRequest{
					Operation: "oper",
					Kind:      "ltm/virtual",
					NewaRef:   nil,
					Name:      "queue cm",
				},
				skipFields: []string{"Timestamp", "Context", "Kind", "NewaRef", "OrigRef", "ObjRefs", "Relevants", "Retries"},
			},
			want: "Operation: oper, Partition: , Subfolder: , Name: queue cm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := struct2string(tt.args.r, tt.args.skipFields); got != tt.want {
				t.Errorf("struct2string() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeployRequest_Enum(t *testing.T) {
	r0 := &DeployRequest{}
	r1 := &DeployRequest{}
	r2 := &DeployRequest{Relevants: []*DeployRequest{r1}}
	r3 := &DeployRequest{Relevants: []*DeployRequest{r2}}
	r4 := &DeployRequest{Relevants: []*DeployRequest{r0, r1}}
	r5 := &DeployRequest{Relevants: []*DeployRequest{r3, r4}}
	r6 := &DeployRequest{}
	r6.Relevants = []*DeployRequest{r6}
	tests := []struct {
		name  string
		input *DeployRequest
		want  []*DeployRequest
	}{
		{
			name:  "empty relevant",
			input: r1,
			want:  []*DeployRequest{r1},
		},
		{
			name:  "1 relevant",
			input: r2,
			want:  []*DeployRequest{r2, r1},
		},
		{
			name:  "serial relevants",
			input: r3,
			want:  []*DeployRequest{r3, r2, r1},
		},
		{
			name:  "parallel relevant",
			input: r4,
			want:  []*DeployRequest{r4, r0, r1},
		},
		{
			name:  "duplicate relevant",
			input: r5,
			want:  []*DeployRequest{r5, r3, r2, r1, r4, r0},
		},
		{
			name:  "loop relevant",
			input: r6,
			want:  []*DeployRequest{r6},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.input

			if got := r.Enum(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v but want %v", got, tt.want)
			}
		})
	}
}

func TestDeployRequest_syncPool(t *testing.T) {
	pl := sync.Pool{}

	slog := utils.NewLog().WithLevel(utils.LogLevel_Type_DEBUG)
	slog.Infof("got: %s", pl.Get()) // got: %!!(MISSING)s(<nil>)
}

func Test_jsonSet(t *testing.T) {
	type args struct {
		data   map[string]interface{}
		pathto []string
		value  interface{}
	}
	type want struct {
		rlt    map[string]interface{}
		errmsg string
	}
	tests := []struct {
		name string
		args args
		want want
	}{
		{
			name: "create",
			args: args{
				data:   map[string]interface{}{},
				pathto: []string{"a"},
				value:  1,
			},
			want: want{rlt: map[string]interface{}{"a": 1}, errmsg: ""},
		},
		{
			name: "update",
			args: args{
				data:   map[string]interface{}{"a": 1},
				pathto: []string{"a"},
				value:  2,
			},
			want: want{rlt: map[string]interface{}{"a": 2}, errmsg: ""},
		},
		{
			name: "create depth=2",
			args: args{
				data:   map[string]interface{}{"a": map[string]interface{}{}},
				pathto: []string{"a", "b"},
				value:  2,
			},
			want: want{rlt: map[string]interface{}{"a": map[string]interface{}{"b": 2}}, errmsg: ""},
		},
		{
			name: "update depth=2",
			args: args{
				data:   map[string]interface{}{"a": map[string]interface{}{"b": 1}},
				pathto: []string{"a", "b"},
				value:  2,
			},
			want: want{rlt: map[string]interface{}{"a": map[string]interface{}{"b": 2}}, errmsg: ""},
		},
		{
			name: "create interface{}",
			args: args{
				data:   map[string]interface{}{},
				pathto: []string{"a"},
				value:  []string{"x"},
			},
			want: want{rlt: map[string]interface{}{"a": []string{"x"}}, errmsg: ""},
		},
		{
			name: "update interface{}",
			args: args{
				data:   map[string]interface{}{"a": 2},
				pathto: []string{"a"},
				value:  []string{"x"},
			},
			want: want{rlt: map[string]interface{}{"a": []string{"x"}}, errmsg: ""},
		},
		{
			name: "create err1",
			args: args{
				data:   map[string]interface{}{"a": 1},
				pathto: []string{"a", "b"},
				value:  []string{"x"},
			},
			want: want{rlt: map[string]interface{}{"a": 1}, errmsg: "invalid kind for m[a], actual type: int"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := jsonSet(tt.args.data, tt.args.pathto, tt.args.value)
			if err != nil && err.Error() != tt.want.errmsg {
				t.Errorf("jsonSet() error = %v, wantErr %v", err, tt.want.errmsg)
			} else if !reflect.DeepEqual(tt.want.rlt, tt.args.data) {
				t.Errorf("jsonSet() data = %v, want = %v", tt.args.data, tt.want.rlt)
			}
		})
	}
}

func Test_jsonGet(t *testing.T) {
	type args struct {
		jsonbody map[string]interface{}
		pathto   []string
	}
	tests := []struct {
		name string
		args args
		want interface{}
	}{
		// TODO: Add test cases.
		{
			name: "get int",
			args: args{
				jsonbody: map[string]interface{}{"a": 1},
				pathto:   []string{"a"},
			},
			want: 1,
		},
		{
			name: "get []string",
			args: args{
				jsonbody: map[string]interface{}{"a": []string{}},
				pathto:   []string{"a"},
			},
			want: []string{},
		},
		{
			name: "get depth=2",
			args: args{
				jsonbody: map[string]interface{}{"a": map[string]interface{}{"b": 1}},
				pathto:   []string{"a", "b"},
			},
			want: 1,
		},
		{
			name: "get nil1",
			args: args{
				jsonbody: map[string]interface{}{"a": 1},
				pathto:   []string{"a", "b"},
			},
			want: nil,
		},
		{
			name: "get nil2",
			args: args{
				jsonbody: map[string]interface{}{"a": 1},
				pathto:   []string{},
			},
			want: nil,
		},
		{
			name: "get nil3",
			args: args{
				jsonbody: map[string]interface{}{"a": 1},
				pathto:   []string{"b"},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonGet(tt.args.jsonbody, tt.args.pathto); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("jsonGet() = %v, want %v", got, tt.want)
			}
		})
	}
}
