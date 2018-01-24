import _ from 'lodash';
import React from 'react';
import { Col, Radio, Row } from 'antd';

export default class PageHeader extends React.Component {
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

  render() {
    return (
      <div className="page-header">
        <Row>
          <Col span={18}>
            {!this.props.header ? null : <h1>{this.props.header}</h1>}
            {!this.props.subHeaderTitle ? null : <div className="subsection-header">{this.props.subHeaderTitle}</div>}
            {!this.props.subHeader ? null : <h1>{this.props.subHeader}</h1>}
          </Col>
          { this.props.hideButtons ? null :
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
          }
        </Row>

        {!this.props.subMessage ? null : <div>{this.props.subMessage}</div>}
      </div>
    );
  }
}
