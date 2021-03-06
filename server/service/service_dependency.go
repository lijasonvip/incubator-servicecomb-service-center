/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package service

import (
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	apt "github.com/apache/incubator-servicecomb-service-center/server/core"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	scerr "github.com/apache/incubator-servicecomb-service-center/server/error"
	"github.com/apache/incubator-servicecomb-service-center/server/mux"
	serviceUtil "github.com/apache/incubator-servicecomb-service-center/server/service/util"
	"golang.org/x/net/context"
)

func (s *MicroServiceService) AddDependenciesForMicroServices(ctx context.Context, in *pb.AddDependenciesRequest) (*pb.AddDependenciesResponse, error) {
	resp, err := s.AddOrUpdateDependencies(ctx, in.Dependencies, false)
	return &pb.AddDependenciesResponse{
		Response: resp,
	}, err
}

func (s *MicroServiceService) CreateDependenciesForMicroServices(ctx context.Context, in *pb.CreateDependenciesRequest) (*pb.CreateDependenciesResponse, error) {
	resp, err := s.AddOrUpdateDependencies(ctx, in.Dependencies, true)
	return &pb.CreateDependenciesResponse{
		Response: resp,
	}, err
}

func (s *MicroServiceService) AddOrUpdateDependencies(ctx context.Context, dependencyInfos []*pb.ConsumerDependency, override bool) (*pb.Response, error) {
	if len(dependencyInfos) == 0 {
		return serviceUtil.BadParamsResponse("Invalid request body.").Response, nil
	}
	domainProject := util.ParseDomainProject(ctx)
	for _, dependencyInfo := range dependencyInfos {
		if len(dependencyInfo.Providers) == 0 || dependencyInfo.Consumer == nil {
			return serviceUtil.BadParamsResponse("Provider is invalid").Response, nil
		}

		util.Logger().Infof("start create dependency, data info %v", dependencyInfo)

		serviceUtil.SetDependencyDefaultValue(dependencyInfo)

		consumerFlag := util.StringJoin([]string{dependencyInfo.Consumer.AppId, dependencyInfo.Consumer.ServiceName, dependencyInfo.Consumer.Version}, "/")
		consumerInfo := pb.DependenciesToKeys([]*pb.DependencyKey{dependencyInfo.Consumer}, domainProject)[0]
		providersInfo := pb.DependenciesToKeys(dependencyInfo.Providers, domainProject)

		rsp := serviceUtil.ParamsChecker(consumerInfo, providersInfo)
		if rsp != nil {
			util.Logger().Errorf(nil, "create dependency failed, conusmer %s: invalid params.%s", consumerFlag, rsp.Response.Message)
			return rsp.Response, nil
		}

		consumerId, err := serviceUtil.GetServiceId(ctx, consumerInfo)
		util.Logger().Debugf("consumerId is %s", consumerId)
		if err != nil {
			util.Logger().Errorf(err, "create dependency failed, consumer %s: get consumer failed.", consumerFlag)
			return pb.CreateResponse(scerr.ErrInternal, err.Error()), err
		}
		if len(consumerId) == 0 {
			util.Logger().Errorf(nil, "create dependency failed, consumer %s: consumer not exist.", consumerFlag)
			return pb.CreateResponse(scerr.ErrServiceNotExists, "Get consumer's serviceId is empty."), nil
		}

		//建立依赖规则，用于维护依赖关系
		lock, err := mux.Lock(mux.GLOBAL_LOCK)
		if err != nil {
			util.Logger().Errorf(err, "create dependency failed, consumer %s: create lock failed.", consumerFlag)
			return pb.CreateResponse(scerr.ErrInternal, err.Error()), err
		}

		var dep serviceUtil.Dependency
		dep.DomainProject = domainProject
		dep.Consumer = consumerInfo
		dep.ProvidersRule = providersInfo
		dep.ConsumerId = consumerId
		if override {
			err = serviceUtil.CreateDependencyRule(ctx, &dep)
		} else {
			err = serviceUtil.AddDependencyRule(ctx, &dep)
		}
		lock.Unlock()

		if err != nil {
			util.Logger().Errorf(err, "create dependency rule failed: consumer %s", consumerFlag)
			return pb.CreateResponse(scerr.ErrInternal, err.Error()), err
		}
		util.Logger().Infof("Create dependency success: consumer %s, %s  from remote %s", consumerFlag, consumerId, util.GetIPFromContext(ctx))
	}
	return pb.CreateResponse(pb.Response_SUCCESS, "Create dependency successfully."), nil
}

func (s *MicroServiceService) GetProviderDependencies(ctx context.Context, in *pb.GetDependenciesRequest) (*pb.GetProDependenciesResponse, error) {
	err := apt.Validate(in)
	if err != nil {
		util.Logger().Errorf(err, "GetProviderDependencies failed for validating parameters failed.")
		return &pb.GetProDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}
	domainProject := util.ParseDomainProject(ctx)
	providerServiceId := in.ServiceId

	provider, err := serviceUtil.GetService(ctx, domainProject, providerServiceId)
	if err != nil {
		util.Logger().Errorf(err, "GetProviderDependencies failed, %s.", providerServiceId)
		return nil, err
	}
	if provider == nil {
		util.Logger().Errorf(err, "GetProviderDependencies failed for provider does not exist, %s.", providerServiceId)
		return &pb.GetProDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Provider does not exist"),
		}, nil
	}

	dr := serviceUtil.NewProviderDependencyRelation(ctx, domainProject, providerServiceId, provider)
	services, err := dr.GetDependencyConsumers()
	if err != nil {
		util.Logger().Errorf(err, "GetProviderDependencies failed.")
		return &pb.GetProDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	util.Logger().Debugf("GetProviderDependencies successfully, providerId is %s.", in.ServiceId)
	return &pb.GetProDependenciesResponse{
		Response:  pb.CreateResponse(pb.Response_SUCCESS, "Get all consumers successful."),
		Consumers: services,
	}, nil
}

func (s *MicroServiceService) GetConsumerDependencies(ctx context.Context, in *pb.GetDependenciesRequest) (*pb.GetConDependenciesResponse, error) {
	err := apt.Validate(in)
	if err != nil {
		util.Logger().Errorf(err, "GetConsumerDependencies failed for validating parameters failed.")
		return &pb.GetConDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}
	consumerId := in.ServiceId
	domainProject := util.ParseDomainProject(ctx)

	consumer, err := serviceUtil.GetService(ctx, domainProject, consumerId)
	if err != nil {
		util.Logger().Errorf(err, "GetConsumerDependencies failed for get consumer failed.")
		return &pb.GetConDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if consumer == nil {
		util.Logger().Errorf(err, "GetConsumerDependencies failed for consumer does not exist, %s.", consumerId)
		return &pb.GetConDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Consumer does not exist"),
		}, nil
	}

	dr := serviceUtil.NewConsumerDependencyRelation(ctx, domainProject, consumerId, consumer)
	services, err := dr.GetDependencyProviders()
	if err != nil {
		util.Logger().Errorf(err, "GetConsumerDependencies failed for get providers failed.")
		return &pb.GetConDependenciesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	util.Logger().Debugf("GetConsumerDependencies successfully, consumerId is %s.", consumerId)
	return &pb.GetConDependenciesResponse{
		Response:  pb.CreateResponse(pb.Response_SUCCESS, "Get all providers successfully."),
		Providers: services,
	}, nil
}
