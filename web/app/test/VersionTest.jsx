import Adapter from 'enzyme-adapter-react-16';
import { BrowserRouter } from 'react-router-dom';
import Enzyme from 'enzyme';
import { expect } from 'chai';
import { mount } from 'enzyme';
import React from 'react';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Version from '../js/components/Version.jsx';

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('Version', () => {
  let curVer = "v1.2.3";
  let newVer = "v2.3.4";

  let component, fetchStub;

  function withPromise(fn) {
    return component.find("Version").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it('renders initial loading message', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
    });

    component = mount(
      <BrowserRouter>
        <Version />
      </BrowserRouter>
    );

    expect(component.find("Version")).to.have.length(1);
    expect(component.html()).to.include("Performing version check...");
  });

  it('renders up to date message when versions match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ version: curVer })
    });

    component = mount(
      <BrowserRouter>
        <Version
          releaseVersion={curVer}
          uuid="fakeuuid" />
      </BrowserRouter>
    );

    return withPromise(() => {
      expect(component.html()).to.include("Conduit is up to date");
    });
  });

  it('renders update message when versions do not match', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ version: newVer })
    });

    component = mount(
      <BrowserRouter>
        <Version
          releaseVersion={curVer}
          uuid="fakeuuid" />
      </BrowserRouter>
    );

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
      statusText: errMsg
    });

    component = mount(
      <BrowserRouter>
        <Version />
      </BrowserRouter>
    );

    return withPromise(() => {
      expect(component.html()).to.include("Version check failed");
      expect(component.html()).to.include(errMsg);
    });
  });
});
