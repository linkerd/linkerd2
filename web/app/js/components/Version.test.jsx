import { ApiHelpers } from './util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import { mount } from 'enzyme';
import React from 'react';
import Sidebar from './Sidebar.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

sinonStubPromise(sinon);

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
          location={{ pathname: ""}}
          api={apiHelpers}
          releaseVersion={curVer}
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    return withPromise(() => {
      expect(component).not.toIncludeText("Conduit is up to date");
      expandSidebar(component);
      expect(component).toIncludeText("Conduit is up to date");
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
          location={{ pathname: ""}}
          api={apiHelpers}
          releaseVersion={curVer}
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    expandSidebar(component);

    return withPromise(() => {
      expect(component).toIncludeText("Conduit is up to date");
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
          location={{ pathname: ""}}
          api={apiHelpers}
          releaseVersion={curVer}
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    expandSidebar(component);

    return withPromise(() => {
      expect(component).toIncludeText(
        `A new version (${newVer}) is available`);
    });
  });

  it('renders error when version check fails', () => {
    let errMsg = "Fake error";

    fetchStub.returnsPromise().resolves({
      ok: false,
      statusText: errMsg
    });

    component = mount(
      <BrowserRouter>
        <Sidebar
          location={{ pathname: ""}}
          api={apiHelpers} />
      </BrowserRouter>
    );

    expandSidebar(component);

    return withPromise(() => {
      expect(component).toIncludeText("Version check failed");
      expect(component).toIncludeText(errMsg);
    });
  });
});
