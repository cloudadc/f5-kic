package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPSMap_set(t *testing.T) {
	pfn := "a/b/c"
	type testcase struct {
		initial SvcEpsMembers
		tocls   string
		toset   SvcEpsMembers
		want    SvcEpsMembers
	}
	A := SvcEpsMembers{}
	for i := 0; i < 5; i++ {
		A = append(A, SvcEpsMember{
			Cluster:    "cluster1",
			SvcKey:     "default/svc1",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("0.0.0.%d", i),
		})
	}
	B := SvcEpsMembers{}
	for i := 0; i < 3; i++ {
		B = append(B, SvcEpsMember{
			Cluster:    "cluster2",
			SvcKey:     "default/svc1",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("1.0.0.%d", i),
		})
	}
	c := SvcEpsMember{
		Cluster:    "cluster1",
		SvcKey:     "default/svc1",
		TargetPort: 1,
		NodePort:   1,
		IpAddr:     "0.0.0.0",
	}
	d := SvcEpsMember{
		Cluster:    "cluster1",
		SvcKey:     "default/svc1",
		TargetPort: 1,
		NodePort:   1,
		IpAddr:     "0.0.0.5",
	}
	tests := []testcase{
		// TODO: Add test cases.
		{ // initial set with startup
			initial: []SvcEpsMember{},
			tocls:   "",
			toset:   A,
			want:    A,
		},
		{ // appending other clusters' members
			initial: A,
			tocls:   "",
			toset:   B,
			want:    append(A, B...),
		},
		{ // len(toset) == 0, unset
			initial: A,
			tocls:   "cluster1",
			toset:   SvcEpsMembers{},
			want:    SvcEpsMembers{},
		},
		{ // unset cluster1
			initial: append(A, B...),
			tocls:   "cluster1",
			toset:   SvcEpsMembers{},
			want:    B,
		},
		{ // cluster == "", replace
			initial: append(A, B...),
			tocls:   "cluster1",
			toset:   SvcEpsMembers{c},
			want:    append(SvcEpsMembers{c}, B...),
		},
		{ // cluster == "", append
			initial: append(A, B...),
			tocls:   "",
			toset:   SvcEpsMembers{d},
			want:    append(A, append(B, d)...),
		},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("PSMap.set-%d", i), func(t *testing.T) {
			pm := PSMap{
				Items: map[string]SvcEpsMembers{},
				mutex: make(chan bool, 1),
			}
			pm.Items[pfn] = tt.initial
			xyz := strings.Split(pfn, "/")
			p, f, n := xyz[0], xyz[1], xyz[2]
			pm.set(n, p, f, tt.tocls, tt.toset)
			if !reflect.DeepEqual(pm.Items[pfn], tt.want) {
				t.Errorf("pm.set %v not equal %v", pm.Items[pfn], tt.want)
			}
		})
	}
}

func TestPSMap_unset(t *testing.T) {
	pfn := "a/b/c"
	type testcase struct {
		initial SvcEpsMembers
		tocls   string
		want    SvcEpsMembers
	}
	A := SvcEpsMembers{}
	for i := 0; i < 5; i++ {
		A = append(A, SvcEpsMember{
			Cluster:    "cluster1",
			SvcKey:     "default/svc1",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("0.0.0.%d", i),
		})
	}
	B := SvcEpsMembers{}
	for i := 0; i < 3; i++ {
		B = append(B, SvcEpsMember{
			Cluster:    "cluster2",
			SvcKey:     "default/svc1",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("1.0.0.%d", i),
		})
	}
	tests := []testcase{
		// TODO: Add test cases.
		{
			initial: A,
			tocls:   "cluster1",
			want:    nil,
		},
		{
			initial: append(A, B...),
			tocls:   "cluster1",
			want:    B,
		},
		{
			initial: append(A, B...),
			tocls:   "",
			want:    append(A, B...),
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("PSMap.unset-%d", i), func(t *testing.T) {
			pm := PSMap{
				Items: map[string]SvcEpsMembers{},
				mutex: make(chan bool, 1),
			}
			pm.Items[pfn] = tt.initial

			xyz := strings.Split(pfn, "/")
			p, f, n := xyz[0], xyz[1], xyz[2]
			pm.unset(n, p, f, tt.tocls)
			if !reflect.DeepEqual(tt.want, pm.Items[pfn]) {
				t.Errorf("pm.unset %v not equal %v", pm.Items[pfn], tt.want)
			}
		})
	}
}

func TestPSMap_get(t *testing.T) {
	pfn := "a/b/c"

	A := SvcEpsMembers{}
	for i := 0; i < 5; i++ {
		A = append(A, SvcEpsMember{
			Cluster:    "cluster1",
			SvcKey:     "default/svc1",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("0.0.0.%d", i),
		})
	}
	B := SvcEpsMembers{}
	for i := 0; i < 3; i++ {
		B = append(B, SvcEpsMember{
			Cluster:    "cluster2",
			SvcKey:     "default/svc2",
			TargetPort: 1,
			NodePort:   1,
			IpAddr:     fmt.Sprintf("1.0.0.%d", i),
		})
	}
	pm := PSMap{
		Items: map[string]SvcEpsMembers{},
		mutex: make(chan bool, 1),
	}
	pm.Items[pfn] = append(A, B...)

	t.Run("PSMap.get after unset", func(t *testing.T) {

		xyz := strings.Split(pfn, "/")
		p, f, n := xyz[0], xyz[1], xyz[2]
		orig := pm.get(n, p, f)
		pm.unset(n, p, f, "cluster1")
		newa := pm.get(n, p, f)
		if reflect.DeepEqual(orig, newa) {
			t.Errorf("pm.get after unset %v should not equal %v", orig, newa)
		}

		empty := pm.get("z", "x", "y")
		if len(empty) != 0 {
			t.Errorf("pm.get x/y/z should be equal to 0")
		}
	})
}
