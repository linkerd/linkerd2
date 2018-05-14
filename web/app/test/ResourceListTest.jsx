import _ from 'lodash';
import Adapter from 'enzyme-adapter-react-16';
import ConduitSpinner from '../js/components/ConduitSpinner.jsx';
import deployRollup from './fixtures/deployRollup.json';
import ErrorBanner from '../js/components/ErrorBanner.jsx';
import { expect } from 'chai';
import MetricsTable from '../js/components/MetricsTable.jsx';
import PageHeader from '../js/components/PageHeader.jsx';
import React from 'react';
import { ResourceListBase } from '../js/components/ResourceList.jsx';
import Enzyme, { shallow } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });

describe('Tests for <ResourceListBase>', () => {
  const defaultProps = {
    resource: 'pods',
  };

  it('displays an error if the api call fails', () => {
    const msg = 'foobar';

    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        error={msg}
        loading={false} />
    );

    const err = component.find(ErrorBanner);
    expect(err).to.have.length(1);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(component.find(MetricsTable)).to.have.length(1);

    expect(err.props().message).to.equal(msg);
  });

  it('shows a loading spinner', () => {
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        loading={true} />
    );

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(0);
    expect(component.find(ConduitSpinner)).to.have.length(1);
    expect(component.find(MetricsTable)).to.have.length(0);
  });

  it('handles empty content', () => {
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        loading={false} />
    );

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(component.find(MetricsTable)).to.have.length(1);
  });

  it('renders a metrics table', () => {
    const resource = 'deployment';
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[deployRollup]}
        loading={false}
        resource={resource} />
    );

    const metrics = component.find(MetricsTable);

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(PageHeader)).to.have.length(1);
    expect(component.find(ConduitSpinner)).to.have.length(0);
    expect(metrics).to.have.length(1);

    expect(metrics.props().resource).to.equal(_.startCase(resource));
    expect(metrics.props().metrics).to.have.length(1);
  });
});
