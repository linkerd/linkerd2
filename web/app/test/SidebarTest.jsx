import Adapter from "enzyme-adapter-react-16";
import ApiHelpers from "../js/components/util/ApiHelpers.jsx";
import { BrowserRouter } from 'react-router-dom';
import { expect } from 'chai';
import multiResourceRollupFixtures from './fixtures/allRollup.json';
import React from "react";
import { Select } from 'antd';
import Sidebar from "../js/components/Sidebar.jsx";
import sinon from "sinon";
import sinonStubPromise from "sinon-stub-promise";
import Enzyme, { mount } from "enzyme";

Enzyme.configure({adapter: new Adapter()});
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
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve(multiResourceRollupFixtures)
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
      expect(component.find(".ant-select-dropdown-menu-item")).to.have.length(2);
    });
  });
});


