import { Table } from 'antd';

// BaseTable extends ant-design's table, but overwrites the `toggleSortOrder`
// method, in order to remove the default behavior of unsorting a column when
// the same sorting arrow is pressed twice:
// https://github.com/ant-design/ant-design/blob/master/components/table/Table.tsx#L348
export default class BaseTable extends Table {
  constructor(props) {
    super(props);
    Table.prototype.toggleSortOrder = this.toggleSortOrder;
  }

  toggleSortOrder(order, column) {
    let { sortColumn, sortOrder } = this.state;

    const newState = {
      sortOrder: order,
      sortColumn: column,
    };

    if (this.getSortOrderColumns().length === 0) {
      this.setState(newState);
    }

    const onChange = this.props.onChange;
    if (onChange) {
      onChange.apply(null, this.prepareParamsArguments({
        ...this.state,
        ...newState,
      }));
    }
  }
}
