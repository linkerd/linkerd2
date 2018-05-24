import _ from 'lodash';
import Adapter from 'enzyme-adapter-react-16';
import CallToAction from '../js/components/CallToAction.jsx';
import ConduitSpinner from '../js/components/ConduitSpinner.jsx';
import deployRollup from './fixtures/deployRollup.json';
import Enzyme from 'enzyme';
import ErrorBanner from '../js/components/ErrorBanner.jsx';
import { expect } from 'chai';
import MetricsTable from '../js/components/MetricsTable.jsx';
import PageHeader from '../js/components/PageHeader.jsx';
import { processSingleResourceRollup } from '../js/components/util/MetricUtils.js';
import React from 'react';
import { ResourceList } from '../js/components/ResourceList.jsx';
import { shallow } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });

describe('Tests for <ResourceList>', () => {
  it('displays an error if the api call fails', () => {
    const msg = 'foobar';

    const component = shallow(
      <ResourceList
        data={[]}
        error={msg}
        loading={false} />
    );

    const err = component.find(ErrorBanner);
    expect(err).to.have.length(1);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(component.find(CallToAction)).to.have.length(1);
    expect(component.find(MetricsTable)).to.have.length(0);

    expect(err.props().message).to.equal(msg);
  });

  it('shows a loading spinner', () => {
    const component = shallow(
      <ResourceList
        data={[]}
        loading={true} />
    );

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(1);
    expect(component.find(CallToAction)).to.have.length(0);
    expect(component.find(MetricsTable)).to.have.length(0);
  });

  it('handles empty content', () => {
    const component = shallow(
      <ResourceList
        data={[]}
        loading={false} />
    );

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(component.find(CallToAction)).to.have.length(1);
    expect(component.find(MetricsTable)).to.have.length(0);
  });

  it('renders a metrics table', () => {
    const resource = 'deployment';
    const component = shallow(
      <ResourceList
        data={[deployRollup]}
        loading={false}
        resource={resource} />
    );

    const metrics = component.find(MetricsTable);

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(component.find(CallToAction)).to.have.length(0);
    expect(metrics).to.have.length(1);

    expect(metrics.props().resource).to.equal(_.startCase(resource));
    expect(metrics.props().metrics).to.have.length(1);
  });
});
