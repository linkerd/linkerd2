import Deployment from '../js/components/Deployment.jsx';
import { expect } from 'chai';
import { mount, shallow } from 'enzyme';
import React from 'react';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

sinonStubPromise(sinon);

describe('Deployment', () => {
  let component, fetchStub;

  function withPromise(fn) {
    return component.get(0).serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    window.fetch.restore();
  });

  it('renders the spinner before metrics are loaded', () => {
    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ metrics: [] })
    });
    component = shallow(<Deployment location={{search:"search"}} />);
    expect(component.find("ConduitSpinner")).to.have.length(1);
  });
});
