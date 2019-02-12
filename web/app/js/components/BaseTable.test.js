import _merge from 'lodash/merge';
import ApiHelpers from './util/ApiHelpers.jsx';
import BaseTable from './BaseTable.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';

describe("Tests for <BaseTable>", () => {
  const defaultProps = {
    api: ApiHelpers(""),
  };

  it("renders the table with sample data", () => {
    let extraProps = _merge({}, defaultProps, {
      tableRows: [{
        name: "authors",
        namespace: "default",
        key: "default-deployment-authors",
        successRate: 0.6956521739130435,
        requestRate: 6.9,
        latency: {P50: 4, P95: 17, P99: 66},
        pods: {totalPods: "1", meshedPods: "1"},
        requestRate: 6.9,
        successRate: 0.6956521739130435,
        totalRequests: 414,
        type: "deployment"
      }],
      tableColumns: [{
        dataIndex: "pods.totalPods",
        title: "Meshed"
      },
      {
        dataIndex: "namespace",
        title: "Namespace"
      }],
    });
    const component = mount(routerWrap(BaseTable, extraProps));

    const table = component.find("BaseTable");

    expect(table).toBeDefined();
    expect(table.props().tableRows).toHaveLength(1);
    expect(table.props().tableColumns).toHaveLength(2);
    expect(table.find("td")).toHaveLength(2);

  });

  it("if enableFilter is true, user is shown the filter dialog", () => {
    let extraProps = _merge({}, defaultProps, {
      tableRows: [{
        name: "authors",
        namespace: "default",
        key: "default-deployment-authors",
        successRate: 0.6956521739130435,
        requestRate: 6.9,
        latency: {P50: 4, P95: 17, P99: 66},
        pods: {totalPods: "1", meshedPods: "1"},
        requestRate: 6.9,
        successRate: 0.6956521739130435,
        totalRequests: 414,
        type: "deployment"
      },{
        name: "books",
        namespace: "user-maria",
        key: "default-deployment-books",
        successRate: 0.565,
        requestRate: 9,
        latency: {P50: 1, P95: 2, P99: 3},
        pods: {totalPods: "1", meshedPods: "1"},
        requestRate: 6.9,
        successRate: 0.6956521739130435,
        totalRequests: 414,
        type: "deployment"
      }],
      tableColumns: [
      {
        dataIndex: "namespace",
        title: "Namespace",
        sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace),
        defaultOrderBy: "asc"
      }],
      enableFilter: true
    });
    const component = mount(routerWrap(BaseTable, extraProps));

    const table = component.find("BaseTable");

    const enableFilter = table.prop("enableFilter");

    const input = table.find("input");

    expect(enableFilter).toEqual(true);
    expect(table.html()).toContain("Filter by text");
  });
});
