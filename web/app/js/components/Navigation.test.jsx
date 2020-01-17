import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import { BrowserRouter } from 'react-router-dom';
import React from 'react';
import mediaQuery from 'css-mediaquery';
import Navigation from './Navigation.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import { mount } from 'enzyme';
import { createMemoryHistory } from 'history';

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

const curVer = "edge-1.2.3";
const newVer = "edge-2.3.4";

const defaultProps = {
  api: ApiHelpers(''),
  checkNamespaceMatch: () => {},
  ChildComponent: () => null,
  classes: {},
  history: createMemoryHistory('/namespaces'),
  location: loc,
  pathPrefix: '',
  releaseVersion: curVer,
  selectedNamespace: 'emojivoto',
  theme: {},
  updateNamespaceInContext: () => {},
  uuid: 'fakeuuid',
};

describe('Navigation', () => {
  let component, fetchStub;

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
        <Navigation {...defaultProps} />
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
        <Navigation {...defaultProps} />
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
        <Navigation {...defaultProps} />
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
    const extraProps = _merge({}, defaultProps, {
      selectedNamespace: '_all',
    });

    const component = mount(
      <BrowserRouter>
        <Navigation {...extraProps} />
      </BrowserRouter>
    );

    const input = component.find("input");
    expect(input.instance().value).toEqual("ALL NAMESPACES");
  });

  it('renders emojivoto text', () => {
    const component = mount(
      <BrowserRouter>
        <Navigation {...defaultProps} />
      </BrowserRouter>
    );

    const input = component.find("input");
    expect(input.instance().value).toEqual("EMOJIVOTO");
  });
});
