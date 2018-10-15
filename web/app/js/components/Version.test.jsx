import Adapter from 'enzyme-adapter-react-16';
import ApiHelpers from './util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import React from 'react';
import Sidebar from './Sidebar.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import { mount } from 'enzyme';

sinonStubPromise(sinon);

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '',
  search: '',
};

describe('Version', () => {
  let curVer = "edge-1.2.3";
  let newVer = "edge-2.3.4";

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
      json: () => Promise.resolve({ edge: curVer })
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
      expect(component).toIncludeText("Linkerd is up to date");
      expandSidebar(component);
      expect(component).not.toIncludeText("Linkerd is up to date");
    });
  });

  it('renders up to date message when versions match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ edge: curVer })
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
      expect(component).toIncludeText("Linkerd is up to date");
    });
  });

  it('renders update message when versions do not match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ edge: newVer })
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
      expect(component).toIncludeText("A new version (2.3.4) is available.");
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

    return withPromise(() => {
      expect(component).toIncludeText("Version check failed: Fake error.");
      expect(component).toIncludeText(errMsg);
    });
  });
});
