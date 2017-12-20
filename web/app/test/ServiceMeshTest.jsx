import _ from 'lodash';
import { expect } from 'chai';
import { mount } from 'enzyme';
import podFixtures from './fixtures/pods.json';
import { routerWrap } from './testHelpers.jsx';
import ServiceMesh from '../js/components/ServiceMesh.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

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

  it("displays an error if the api call didn't go well", () => {
    let errorMsg = "Something went wrong!";

    fetchStub.returnsPromise().resolves({
      ok: false,
      statusText: errorMsg
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include(errorMsg);
    });
  });

  it("displays message for no deployments detetcted", () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ pods: []})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include("No deployments detected.");
    });
  });

  it("displays message for more than one deployment added to servicemesh", () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ pods: podFixtures.pods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include("deployments have not been added to the service mesh.");
    });
  });

  it("displays message for only one deployment not added to servicemesh", () => {
    let addedPods = _.clone(podFixtures.pods);
    _.set(addedPods[0], "added", true);

    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ pods: addedPods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include("1 deployment has not been added to the service mesh.");
    });
  });

  it("displays message for all deployments added to servicemesh", () => {
    let addedPods = _.clone(podFixtures.pods);
    _.forEach(addedPods, pod => {
      _.set(pod, "added", true);
    });

    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ pods: addedPods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include("All deployments have been added to the service mesh.");
    });
  });
});
