import _ from 'lodash';
import Adapter from 'enzyme-adapter-react-16';
import Enzyme from 'enzyme';
import { expect } from 'chai';
import { mount } from 'enzyme';
import podFixtures from './fixtures/pods.json';
import { routerWrap } from './testHelpers.jsx';
import ServiceMesh from '../js/components/ServiceMesh.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('ServiceMesh', () => {
  let component, fetchStub;

  function withPromise(fn) {
    return component.find("ServiceMesh").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch').returnsPromise();
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it("displays an error if the api call didn't go well", () => {
    let errorMsg = "Something went wrong!";

    fetchStub.resolves({
      ok: false,
      statusText: errorMsg
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include(errorMsg);
    });
  });

  it("renders the spinner before metrics are loaded", () => {
    component = mount(routerWrap(ServiceMesh));

    expect(component.find("ConduitSpinner")).to.have.length(1);
    expect(component.find("ServiceMesh")).to.have.length(1);
    expect(component.find("CallToAction")).to.have.length(0);
  });

  it("renders a call to action if no metrics are received", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.find("CallToAction")).to.have.length(1);
    });
  });

  it("renders controller component summaries", () => {
    let addedPods = _.cloneDeep(podFixtures.pods);
    _.set(addedPods[0], "added", true);

    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ pods: addedPods})
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.find("DeploymentSummary")).to.have.length(3);
    });
  });

  it("renders service mesh details section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.html()).to.include("Service mesh details");
      expect(component.html()).to.include("Conduit version");
    });
  });

  it("renders control plane section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.html()).to.include("Control plane");
    });
  });

  it("renders data plane section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.html()).to.include("Data plane");
    });
  });

  describe("renderAddDeploymentsMessage", () => {
    it("displays when no deployments are in the mesh", () => {
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({ pods: []})
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("No deployments detected.");
      });
    });

    it("displays a message if >1 deployment has not been added to the mesh", () => {
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({ pods: podFixtures.pods})
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("deployments have not been added to the service mesh.");
      });
    });

    it("displays a message if 1 deployment has not added to servicemesh", () => {
      let addedPods = _.cloneDeep(podFixtures.pods);
      _.set(addedPods[0], "added", true);

      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({ pods: addedPods})
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("1 deployment has not been added to the service mesh.");
      });
    });

    it("displays a message if all deployments have been added to servicemesh", () => {
      let addedPods = _.cloneDeep(podFixtures.pods);
      _.forEach(addedPods, pod => {
        _.set(pod, "added", true);
      });

      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({ pods: addedPods})
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("All deployments have been added to the service mesh.");
      });
    });
  });
});
