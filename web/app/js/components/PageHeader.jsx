import _ from 'lodash';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { Col, Radio, Row } from 'antd';

class PageHeader extends React.Component {
  constructor(props) {
    super(props);
    this.onTimeWindowClick = this.onTimeWindowClick.bind(this);
    this.state = {
      selectedWindow: this.props.api.getMetricsWindow()
    };
  }

  onTimeWindowClick(e) {
    let window = e.target.value;
    this.props.api.setMetricsWindow(window);
    this.setState({selectedWindow: window});
  }

  // don't use time window changing until the results of Telemetry Scalability are in
  // https://github.com/runconduit/conduit/milestone/4
  renderMetricWindowButtons() {
    if (this.props.hideButtons) {
      return null;
    } else {
      return (
        <Col span={6}>
          <Radio.Group
            className="time-window-btns"
            value={this.state.selectedWindow}
            onChange={this.onTimeWindowClick} >
            {
              _.map(this.props.api.getValidMetricsWindows(), (w, i) => {
                return <Radio.Button key={`metrics-window-btn-${i}`} value={w}>{w}</Radio.Button>;
              })
            }
          </Radio.Group>
        </Col>
      );
    }
  }

  render() {
    return (
      <div className="page-header">
        <Row>
          <Col span={18}>
            {!this.props.header ? null : <h1>{this.props.header}</h1>}
            {!this.props.subHeaderTitle ? null : <div className="subsection-header">{this.props.subHeaderTitle}</div>}
            {!this.props.subHeader ? null : <h1>{this.props.subHeader}</h1>}
          </Col>
          {/* {this.renderMetricWindowButtons()} */}
        </Row>

        {!this.props.subMessage ? null : <div>{this.props.subMessage}</div>}
      </div>
    );
  }
}

export default withContext(PageHeader);
