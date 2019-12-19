import ApiHelpers from './util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import React from 'react';
import mediaQuery from 'css-mediaquery';
import Menu from '@material-ui/core/Menu';
import MenuItem from '@material-ui/core/MenuItem';
import Navigation from './Navigation.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import { mount } from 'enzyme';

function createMatchMedia(width) {
  return query => ({
    matches: mediaQuery.match(query, { width }),
    addListener: () => {},
    removeListener: () => {},
  });
}

sinonStubPromise(sinon);

const loc = {
  pathname: '',
  hash: '',
  pathPrefix: '',
  search: '',
};

const namespaces = [
  {key: "-namespace-default", name: "default", namespace: "", type: "namespace"},
  {key: "-namespace-emojivoto", name: "emojivoto", namespace: "", type: "namespace"},
  {key: "-namespace-linkerd", name: "linkerd", namespace: "", type: "namespace"},
];

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

  it('checks state when versions match', () => {
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
      expect(component.find("NavigationBase").state("isLatest")).toBeTruthy();
      expect(component.find("NavigationBase").state("latestVersion")).toBe(curVer);
    });
  });

  it('checks state when versions do not match', () => {
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
      expect(component.find("NavigationBase").state("isLatest")).toBeFalsy();
      expect(component.find("NavigationBase").state("latestVersion")).toBe(newVer);
    });
  });

  it('checks state when version check fails', () => {
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
      expect(component.find("NavigationBase").state("error")).toBeDefined();
      expect(component.find("NavigationBase").state("error").statusText).toBe("Fake error");
    });
  });
});

describe('Namespace Select Button', () => {
  beforeEach(() => {
    // https://material-ui.com/components/use-media-query/#testing
    window.matchMedia = createMatchMedia(window.innerWidth);
  });

  it('displays All Namespaces as button text if the selected namespace is _all', () => {
    const component = mount(
      <BrowserRouter>
        <Navigation
          ChildComponent={() => null}
          classes={{}}
          theme={{}}
          location={loc}
          api={ApiHelpers("")}
          releaseVersion="edge-1.2.3"
          selectedNamespace="_all"
          pathPrefix=""
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    const input = component.find("input");
    expect(input.instance().value).toEqual("ALL NAMESPACES")
  });

  it('renders emojivoto text', () => {
    const component = mount(
      <BrowserRouter>
        <Navigation
          ChildComponent={() => null}
          classes={{}}
          theme={{}}
          location={loc}
          api={ApiHelpers("")}
          releaseVersion="edge-1.2.3"
          selectedNamespace="emojivoto"
          pathPrefix=""
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    const input = component.find("input");
    expect(input.instance().value).toEqual("EMOJIVOTO")
  });
});
