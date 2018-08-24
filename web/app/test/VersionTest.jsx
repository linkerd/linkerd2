import Adapter from 'enzyme-adapter-react-16';
import ApiHelpers from '../js/components/util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import { expect } from 'chai';
import React from 'react';
import Sidebar from '../js/components/Sidebar.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Enzyme, { mount } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '',
  search: '',
};

describe('Version', () => {
  let curVer = "v1.2.3";
  let newVer = "v2.3.4";

  let component, fetchStub;
  let apiHelpers = ApiHelpers("");

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

  const expandSidebar = component => {
    // click trigger to expand the sidebar
    component.find(".ant-layout-sider-trigger").simulate('click');
  };

  it('is hidden when the sidebar is collapsed', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ version: curVer })
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
      expect(component.html()).not.to.include("Linkerd is up to date");
      expandSidebar(component);
      expect(component.html()).to.include("Linkerd is up to date");
    });
  });

  it('renders up to date message when versions match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ version: curVer })
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

    expandSidebar(component);

    return withPromise(() => {
      expect(component.html()).to.include("Linkerd is up to date");
    });
  });

  it('renders update message when versions do not match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ version: newVer })
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

    expandSidebar(component);

    return withPromise(() => {
      expect(component.html()).to.include("A new version (");
      expect(component.html()).to.include(newVer);
      expect(component.html()).to.include(") is available");
    });
  });

  it('renders error when version check fails', () => {
    let errMsg = "Fake error";

    fetchStub.returnsPromise().resolves({
      ok: false,
      json: () => Promise.resolve({
        error: errMsg
      }),
      statusText: errMsg,
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

    expandSidebar(component);

    return withPromise(() => {
      expect(component.html()).to.include("Version check failed");
      expect(component.html()).to.include(errMsg);
    });
  });
});
