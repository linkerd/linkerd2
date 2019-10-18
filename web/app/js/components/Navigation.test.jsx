import ApiHelpers from './util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import React from 'react';
import Navigation from './Navigation.jsx';
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


describe('Navigation', () => {
  let curVer = "edge-1.2.3";
  let newVer = "edge-2.3.4";
  let selectedNamespace = "emojivoto"

  let component, fetchStub;
  let apiHelpers = ApiHelpers("");
  const childComponent = () => null;

  function withPromise(fn) {
    return component.find("NavigationBase").instance().versionPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it('renders up to date message when versions match', () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ edge: curVer })
    });

    component = mount(
      <BrowserRouter>
        <Navigation
          ChildComponent={childComponent}
          classes={{}}
          theme={{}}
          location={loc}
          api={apiHelpers}
          releaseVersion={curVer}
          selectedNamespace={selectedNamespace}
          pathPrefix=""
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    return withPromise(() => {
      expect(component).toIncludeText("Linkerd is up to date");
    });
  });

  it('renders update message when versions do not match', () => {
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({ edge: newVer })
    });

    component = mount(
      <BrowserRouter>
        <Navigation
          ChildComponent={childComponent}
          classes={{}}
          theme={{}}
          location={loc}
          api={apiHelpers}
          releaseVersion={curVer}
          selectedNamespace={selectedNamespace}
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

    fetchStub.rejects({
      ok: false,
      json: () => Promise.resolve({
        error: {},
      }),
      statusText: errMsg,
    });

    component = mount(
      <BrowserRouter>
        <Navigation
          ChildComponent={childComponent}
          classes={{}}
          theme={{}}
          location={loc}
          api={apiHelpers}
          releaseVersion={curVer}
          selectedNamespace={selectedNamespace}
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
