import _ from 'lodash';
import conduitPodFixtures from '../test/fixtures/conduitPods.json';
import { mount } from 'enzyme';
import nsFixtures from '../test/fixtures/namespaces.json';
import { routerWrap } from '../test/helpers.jsx';
import ServiceMesh from './ServiceMesh.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

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
      expect(component).toIncludeText(errorMsg);
    });
  });

  it("renders the spinner before metrics are loaded", () => {
    component = mount(routerWrap(ServiceMesh));

    expect(component.find("ConduitSpinner")).toHaveLength(1);
    expect(component.find("ServiceMesh")).toHaveLength(1);
    expect(component.find("CallToAction")).toHaveLength(0);
  });

  it("renders a call to action if no metrics are received", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).toHaveLength(1);
      expect(component.find("ConduitSpinner")).toHaveLength(0);
      expect(component.find("CallToAction")).toHaveLength(1);
    });
  });

  it("renders controller component summaries", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve(conduitPodFixtures)
    });
    component = mount(routerWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).toHaveLength(1);
      expect(component.find("ConduitSpinner")).toHaveLength(0);
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
      expect(component.find("ServiceMesh")).toHaveLength(1);
      expect(component.find("ConduitSpinner")).toHaveLength(0);
      expect(component).toIncludeText("Service mesh details");
      expect(component).toIncludeText("Conduit version");
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
      expect(component.find("ServiceMesh")).toHaveLength(1);
      expect(component.find("ConduitSpinner")).toHaveLength(0);
      expect(component).toIncludeText("Control plane");
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
      expect(component.find("ServiceMesh")).toHaveLength(1);
      expect(component.find("ConduitSpinner")).toHaveLength(0);
      expect(component).toIncludeText("Data plane");
    });
  });

  describe("renderAddDeploymentsMessage", () => {
    it("displays when no resources are in the mesh", () => {
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({})
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component).toIncludeText("No resources detected");
      });
    });

    it("displays a message if >1 resource has not been added to the mesh", () => {
      let nsAllResourcesAdded = _.cloneDeep(nsFixtures);
      nsAllResourcesAdded.ok.statTables[0].podGroup.rows.push({
        "resource":{
          "namespace":"",
          "type":"namespaces",
          "name":"test-1"
        },
        "timeWindow": "1m",
        "meshedPodCount": "0",
        "runningPodCount": "5",
        "stats": null
      });

      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve(nsAllResourcesAdded)
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component).toIncludeText("2 namespaces have no meshed resources.");
      });
    });

    it("displays a message if 1 resource has not added to servicemesh", () => {
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve(nsFixtures)
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component).toIncludeText("1 namespace has no meshed resources.");
      });
    });

    it("displays a message if all resource have been added to servicemesh", () => {
      let nsAllResourcesAdded = _.cloneDeep(nsFixtures);
      _.each(nsAllResourcesAdded.ok.statTables[0].podGroup.rows, row => {
        if (row.resource.name === "default") {
          row.meshedPodCount = "10";
          row.runningPodCount = "10";
        }
      });
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve(nsAllResourcesAdded)
      });
      component = mount(routerWrap(ServiceMesh));

      return withPromise(() => {
        expect(component).toIncludeText("All namespaces have a conduit install.");
      });
    });
  });
});
