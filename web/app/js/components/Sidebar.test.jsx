import ApiHelpers from "./util/ApiHelpers.jsx";
import { BrowserRouter } from 'react-router-dom';
import namespaceFixtures from '../../test/fixtures/namespaces.json';
import React from "react";
import { Select } from 'antd';
import Sidebar from "./Sidebar.jsx";
import sinon from "sinon";
import sinonStubPromise from "sinon-stub-promise";
import { mount } from "enzyme";

sinonStubPromise(sinon);

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '',
  search: '',
};

describe('Sidebar', () => {
  let curVer = "v1.2.3";
  let component, fetchStub;
  let apiHelpers = ApiHelpers("");

  const openNamespaceSelector = component => {
    // click trigger to expand the namespace selector
    component.find(Select).simulate('click');
  };

  function withPromise(fn) {
    return component.find("Sidebar").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it("namespace selector has options", () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve(namespaceFixtures)
    });

    component = mount(
      <BrowserRouter>
        <Sidebar
          location={loc}
          api={apiHelpers}
          releaseVersion={curVer}
          pathPrefix=""
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    return withPromise(() => {
      openNamespaceSelector(component);

      // number of namespaces in api result
      expect(namespaceFixtures.ok.statTables[0].podGroup.rows).toHaveLength(5);

      // plus "All namespaces" option
      expect(component.find(".ant-select-dropdown-menu-item")).toHaveLength(6);
    });
  });
});
