import deployRollup from '../../test/fixtures/deployRollup.json';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import React from 'react';
import { ResourceListBase } from './ResourceList.jsx';
import Spinner from './util/Spinner.jsx';
import { shallow } from 'enzyme';

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
    expect(err).toHaveLength(1);
    expect(component.find(Spinner)).toHaveLength(0);
    expect(component.find(MetricsTable)).toHaveLength(2);
    expect(err.props().message.statusText).toEqual(msg);
  });

  it('shows a loading spinner', () => {
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        loading={true} />
    );

    expect(component.find(ErrorBanner)).toHaveLength(0);
    expect(component.find(Spinner)).toHaveLength(1);
    expect(component.find(MetricsTable)).toHaveLength(0);
  });

  it('handles empty content', () => {
    const component = shallow(
      <ResourceListBase
        {...defaultProps}
        data={[]}
        loading={false} />
    );

    expect(component.find(ErrorBanner)).toHaveLength(0);
    expect(component.find(Spinner)).toHaveLength(0);
    expect(component.find(MetricsTable)).toHaveLength(2);
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

    expect(component.find(ErrorBanner)).toHaveLength(0);
    expect(component.find(Spinner)).toHaveLength(0);
    expect(metrics).toHaveLength(2);

    expect(metrics.at(0).props().resource).toEqual(resource);
    expect(metrics.at(1).props().resource).toEqual(resource);
    expect(metrics.at(0).props().metrics).toHaveLength(1);
    expect(metrics.at(1).props().metrics).toHaveLength(1);
  });
});
