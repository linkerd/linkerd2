import ApiHelpers from './util/ApiHelpers.jsx';
import BaseTable from './BaseTable.jsx';
import { MetricsTableBase } from './MetricsTable.jsx';
import React from 'react';
import { shallow } from 'enzyme';

describe('Tests for <MetricsTableBase>', () => {
  const defaultProps = {
    api: ApiHelpers(''),
  };

  it('renders the table with all columns', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[{
          name: 'web',
          namespace: 'default',
          totalRequests: 0,
        }]}
        resource="deployment" />
    );

    const table = component.find(BaseTable);

    expect(table).toHaveLength(1);
    expect(table.props().dataSource).toHaveLength(1);
    expect(table.props().columns).toHaveLength(10);
  });

  it('omits the namespace column for the namespace resource', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="namespace" />
    );

    const table = component.find(BaseTable);

    expect(table).toHaveLength(1);
    expect(table.props().columns).toHaveLength(9);
  });

  it('omits the namespace column when showNamespaceColumn is false', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="deployment"
        showNamespaceColumn={false} />
    );

    const table = component.find(BaseTable);

    expect(table).toHaveLength(1);
    expect(table.props().columns).toHaveLength(9);
  });

  it('omits meshed column and grafana column for authority resource', () => {
    const component = shallow(
      <MetricsTableBase
        {...defaultProps}
        metrics={[]}
        resource="authority" />
    );

    const table = component.find(BaseTable);

    expect(table).toHaveLength(1);
    expect(table.props().columns).toHaveLength(8);
  });

});
