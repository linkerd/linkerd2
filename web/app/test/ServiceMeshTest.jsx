import ServiceMesh from '../js/components/ServiceMesh.jsx';
import { expect } from 'chai';
import { mount, shallow } from 'enzyme';
import podFixtures from './fixtures/pods.json';
import React from 'react';
import sinon from 'sinon';
import _ from 'lodash';
import sinonStubPromise from 'sinon-stub-promise';
import { printStack, routerWrap } from './testHelpers.jsx';

sinonStubPromise(sinon);

describe('ServiceMesh', () => {
  let component, fetchStub;

  function withPromise(fn) {
    return component.find("ServiceMesh").get(0).serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    window.fetch.restore();
  });

  it("displays message for no deployments detetcted", () => {
    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ pods: []})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).includes("No deployments detected.");
    });
  });

  it("displays message for more than one deployment added to servicemesh", () => {
    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ pods: podFixtures.pods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).includes("deployments have not been added to the service mesh.");
    });
  });

  it("displays message for only one deployment not added to servicemesh", () => {
    let addedPods = _.clone(podFixtures.pods);
    _.set(addedPods[0], "added", true);

    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ pods: addedPods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).includes("1 deployment has not been added to the service mesh.");
    });
  });

  it("displays message for all deployments added to servicemesh", () => {
    let addedPods = _.clone(podFixtures.pods);
    _.forEach(addedPods, pod => {
      _.set(pod, "added", true);
    });

    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ pods: addedPods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).includes("All deployments have been added to the service mesh.");
    });
  });
});