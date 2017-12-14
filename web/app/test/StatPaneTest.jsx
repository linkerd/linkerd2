import { expect } from 'chai';
import { mount } from 'enzyme';
import React from 'react';
import StatPane from '../js/components/StatPane.jsx';


describe('StatPane', () => {
  it('renders the request, success rate and latency components', () => {
    let component = mount(
      <StatPane
        lastUpdated={Date.now()}
        timeseries={[]} />
    );

    expect(component.find(".border-container").length).to.equal(2);
    expect(component.find(".line-graph").length).to.equal(3);
    expect(component.find(".current-latency").length).to.equal(1);
  });
});
