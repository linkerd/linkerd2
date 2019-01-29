import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import MetricsTable from './MetricsTable.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';

describe('Tests for <MetricsTable>', () => {
  const defaultProps = {
    api: ApiHelpers(''),
  };

  it('renders the table with all columns', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [{
        name: 'web',
        namespace: 'default',
        key: 'web-default-deploy',
        totalRequests: 0,
      }],
      resource: "deployment"
    });
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableRows).toHaveLength(1);
    expect(table.props().tableColumns).toHaveLength(10);
  });

  it('omits the namespace column for the namespace resource', () => {
    let extraProps = _merge({}, defaultProps, { metrics: [], resource: "namespace"});
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(9);
  });

  it('omits the namespace column when showNamespaceColumn is false', () => {
    let extraProps = _merge({}, defaultProps, {
      metrics: [],
      resource: "deployment",
      showNamespaceColumn: false
    });
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(9);
  });

  it('omits meshed column for an authority resource', () => {
    let extraProps = _merge({}, defaultProps, { metrics: [], resource: "authority"});
    const component = mount(routerWrap(MetricsTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableColumns).toHaveLength(9);
  });
});
