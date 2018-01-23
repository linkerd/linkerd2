import Adapter from 'enzyme-adapter-react-16';
import Enzyme from 'enzyme';
import { expect } from 'chai';
import { mount } from 'enzyme';
import React from 'react';
import ResourceMetricsOverview from '../js/components/ResourceMetricsOverview.jsx';

Enzyme.configure({ adapter: new Adapter() });

describe('ResourceMetricsOverview', () => {
  it('renders the request, success rate and latency components', () => {
    let component = mount(
      <ResourceMetricsOverview
        lastUpdated={Date.now()}
        timeseries={[]} />
    );

    expect(component.find(".border-container").length).to.equal(2);
    expect(component.find(".line-graph").length).to.equal(3);
    expect(component.find(".current-latency").length).to.equal(1);
  });
});
