/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/data"
)

var _ = Describe("alarm Notification handler", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			handler, err := NewAlarmNotificationHandler().
				SetCloudID("123").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("logger"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})

		It("Can't be created without a cloud identifier", func() {
			handler, err := NewAlarmNotificationHandler().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(handler).To(BeNil())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("cloud identifier"))
			Expect(msg).To(ContainSubstring("mandatory"))
		})
	})

	Describe("Behaviour", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			// Create a context:
			ctx = context.Background()

		})

		Describe("Post", func() {
			It("Post an alarm notification", func() {
				// Create the handler:
				handler, err := NewAlarmNotificationHandler().
					SetLogger(logger).
					SetCloudID("123").
					Build()
				Expect(err).ToNot(HaveOccurred())
				obj := data.Object{
					"customerId": "test_custer_id",
					"filter": data.Object{
						"notificationType": "1",
						"nsInstanceId":     "test_instance_id",
						"status":           "active",
					},
				}

				//add the request
				add_req := AddRequest{nil, obj}
				resp, err := handler.Add(ctx, &add_req)
				Expect(err).ToNot(HaveOccurred())

				//decode the subId
				sub_id, err := handler.decodeSubId(ctx, resp.Object)
				Expect(err).ToNot(HaveOccurred())

				//use Get to verify the addrequest
				get_resp, err := handler.Get(ctx, &GetRequest{
					Variables: []string{sub_id},
				})
				Expect(err).ToNot(HaveOccurred())
				//extract sub_id and verify
				sub_id_get, err := handler.decodeSubId(ctx, get_resp.Object)
				Expect(err).ToNot(HaveOccurred())
				Expect(sub_id).To(Equal(sub_id_get))

				//use Delete
				_, err = handler.Delete(ctx, &DeleteRequest{
					Variables: []string{sub_id}})
				Expect(err).ToNot(HaveOccurred())

				//use Get to verify the entry was deleted
				get_resp, err = handler.Get(ctx, &GetRequest{
					Variables: []string{sub_id},
				})

				msg := err.Error()
				Expect(msg).To(Equal("not found"))
				Expect(get_resp.Object).To(BeEmpty())
			})

		})
	})
})
