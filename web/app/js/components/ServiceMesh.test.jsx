import _cloneDeep from 'lodash/cloneDeep';
import _each from 'lodash/each';
import { expect } from 'chai';
import nsFixtures from '../../test/fixtures/namespaces.json';
import podFixtures from '../../test/fixtures/podRollup.json';
import { i18nAndRouterWrap } from '../../test/testHelpers.jsx';
import ServiceMesh from './ServiceMesh.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Spinner from './util/Spinner.jsx';
import { mount } from 'enzyme';

sinonStubPromise(sinon);

describe('ServiceMesh', () => {
  let component, fetchStub;

  // https://material-ui.com/style/typography/#migration-to-typography-v2
  window.__MUI_USE_NEXT_TYPOGRAPHY_VARIANTS__ = true;

  function withPromise(fn) {
    return component.find("ServiceMesh").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it("displays an error if the api call didn't go well", () => {
    let errorMsg = "Something went wrong!";

    fetchStub.resolves({
      ok: false,
      json: () => Promise.resolve({
        error: errorMsg
      }),
      statusText: errorMsg
    });

    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      expect(component.html()).to.include(errorMsg);
    });
  });

  it("renders the spinner before metrics are loaded", () => {
    component = mount(i18nAndRouterWrap(ServiceMesh));

    expect(component.find(Spinner)).to.have.length(1);
    expect(component.find("ServiceMesh")).to.have.length(1);
    expect(component.find("CallToAction")).to.have.length(0);
  });

  it("renders a call to action if no metrics are received", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      component.update();

      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find(Spinner)).to.have.length(0);
      expect(component.find("CallToAction")).to.have.length(1);
    });
  });

  it("renders controller component summaries", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve(podFixtures)
    });
    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find(Spinner)).to.have.length(0);
    });
  });

  it("renders service mesh details section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find(Spinner)).to.have.length(0);
      expect(component.html()).to.include("Service mesh details");
      expect(component.html()).to.include("ShinyProductName version");
    });
  });

  it("renders control plane section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find(Spinner)).to.have.length(0);
      expect(component.html()).to.include("Control plane");
    });
  });

  it("renders data plane section", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(i18nAndRouterWrap(ServiceMesh));

    return withPromise(() => {
      component.update();
      expect(component.find("ServiceMesh")).to.have.length(1);
      expect(component.find(Spinner)).to.have.length(0);
      expect(component.html()).to.include("Data plane");
    });
  });

  describe("renderAddDeploymentsMessage", () => {
    it("displays when no resources are in the mesh", () => {
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve({})
      });
      component = mount(i18nAndRouterWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("No namespaces detected");
      });
    });

    it("displays a message if >1 resource has not been added to the mesh", () => {
      let nsAllResourcesAdded = _cloneDeep(nsFixtures);
      nsAllResourcesAdded.ok.statTables[0].podGroup.rows.push({
        "resource": {
          "namespace": "",
          "type": "namespace",
          "name": "test-1"
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
      component = mount(i18nAndRouterWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("2 namespaces have no meshed resources.");
      });
    });

    it("displays a message if 1 resource has not added to servicemesh", () => {
      let nsOneResourceNotAdded = _cloneDeep(nsFixtures);
      nsOneResourceNotAdded.ok.statTables[0].podGroup.rows.forEach(row => {
        // set all namespaces to have fully meshed pod counts, except one
        if (row.resource.name !== "shiny-product-unmeshed") {
          row.meshedPodCount = "10";
          row.runningPodCount = "10";
        }
      })
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve(nsOneResourceNotAdded)
      });
      component = mount(i18nAndRouterWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("1 namespace has no meshed resources.");
      });
    });

    it("displays a message if all resources have been added to servicemesh", () => {
      let nsAllResourcesAdded = _cloneDeep(nsFixtures);
      _each(nsAllResourcesAdded.ok.statTables[0].podGroup.rows, row => {
        row.meshedPodCount = "10";
        row.runningPodCount = "10";
      });
      fetchStub.resolves({
        ok: true,
        json: () => Promise.resolve(nsAllResourcesAdded)
      });
      component = mount(i18nAndRouterWrap(ServiceMesh));

      return withPromise(() => {
        expect(component.html()).to.include("All namespaces have a ShinyProductName install.");
      });
    });
  });
});
