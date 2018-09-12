import Adapter from 'enzyme-adapter-react-16';
import deployRollup from './fixtures/deployRollup.json';
import ErrorBanner from '../js/components/ErrorBanner.jsx';
import { expect } from 'chai';
import MetricsTable from '../js/components/MetricsTable.jsx';
import React from 'react';
import { ResourceListBase } from '../js/components/ResourceList.jsx';
import { Spin } from 'antd';
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
        error={{ statusText: msg}}
        loading={false} />
    );

    const err = component.find(ErrorBanner);
    expect(err).to.have.length(1);
    expect(component.find(Spin)).to.have.length(0);
    expect(component.find(MetricsTable)).to.have.length(1);
    expect(err.props().message.statusText).to.equal(msg);
  });

  it('shows a loading spinner', () => {
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        loading={true} />
    );

    expect(component.find(ErrorBanner)).to.have.length(0);
    expect(component.find(Spin)).to.have.length(1);
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
    expect(component.find(Spin)).to.have.length(0);
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
    expect(component.find(Spin)).to.have.length(0);
    expect(metrics).to.have.length(1);

    expect(metrics.props().resource).to.equal(resource);
    expect(metrics.props().metrics).to.have.length(1);
  });
});
